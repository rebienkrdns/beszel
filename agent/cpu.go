package agent

import (
	"bufio"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/shirou/gopsutil/v4/cpu"
)

var lastCpuTimes = make(map[uint16]cpu.TimesStat)
var lastPerCoreCpuTimes = make(map[uint16][]cpu.TimesStat)

// init initializes the CPU monitoring by storing the initial CPU times
// for the default 60-second cache interval.
func init() {
	if times, err := cpu.Times(false); err == nil && len(times) > 0 {
		lastCpuTimes[60000] = times[0]
	}
	if perCoreTimes, err := cpu.Times(true); err == nil && len(perCoreTimes) > 0 {
		lastPerCoreCpuTimes[60000] = perCoreTimes
	}
}

// CpuMetrics contains detailed CPU usage breakdown
type CpuMetrics struct {
	Total  float64
	User   float64
	System float64
	Iowait float64
	Steal  float64
	Idle   float64
}

// getCpuMetrics calculates detailed CPU usage metrics using cached previous measurements.
// It returns percentages for total, user, system, iowait, and steal time.
func getCpuMetrics(cacheTimeMs uint16) (CpuMetrics, error) {
	times, err := cpu.Times(false)
	if err != nil || len(times) == 0 {
		return CpuMetrics{}, err
	}
	// if cacheTimeMs is not in lastCpuTimes, use 60000 as fallback lastCpuTime
	if _, ok := lastCpuTimes[cacheTimeMs]; !ok {
		lastCpuTimes[cacheTimeMs] = lastCpuTimes[60000]
	}

	t1 := lastCpuTimes[cacheTimeMs]
	t2 := times[0]

	t1All, _ := getAllBusy(t1)
	t2All, _ := getAllBusy(t2)

	totalDelta := t2All - t1All
	if totalDelta <= 0 {
		return CpuMetrics{}, nil
	}

	metrics := CpuMetrics{
		Total:  calculateBusy(t1, t2),
		User:   clampPercent((t2.User - t1.User) / totalDelta * 100),
		System: clampPercent((t2.System - t1.System) / totalDelta * 100),
		Iowait: clampPercent((t2.Iowait - t1.Iowait) / totalDelta * 100),
		Steal:  clampPercent((t2.Steal - t1.Steal) / totalDelta * 100),
		Idle:   clampPercent((t2.Idle - t1.Idle) / totalDelta * 100),
	}

	lastCpuTimes[cacheTimeMs] = times[0]
	return metrics, nil
}

// clampPercent ensures the percentage is between 0 and 100
func clampPercent(value float64) float64 {
	return math.Min(100, math.Max(0, value))
}

// getPerCoreCpuUsage calculates per-core CPU busy usage as integer percentages (0-100).
// It uses cached previous measurements for the provided cache interval.
func getPerCoreCpuUsage(cacheTimeMs uint16) (system.Uint8Slice, error) {
	perCoreTimes, err := cpu.Times(true)
	if err != nil || len(perCoreTimes) == 0 {
		return nil, err
	}

	// Initialize cache if needed
	if _, ok := lastPerCoreCpuTimes[cacheTimeMs]; !ok {
		lastPerCoreCpuTimes[cacheTimeMs] = lastPerCoreCpuTimes[60000]
	}

	lastTimes := lastPerCoreCpuTimes[cacheTimeMs]

	// Limit to the number of cores available in both samples
	length := min(len(lastTimes), len(perCoreTimes))

	usage := make([]uint8, length)
	for i := 0; i < length; i++ {
		t1 := lastTimes[i]
		t2 := perCoreTimes[i]
		usage[i] = uint8(math.Round(calculateBusy(t1, t2)))
	}

	lastPerCoreCpuTimes[cacheTimeMs] = perCoreTimes
	return usage, nil
}

// calculateBusy calculates the CPU busy percentage between two time points.
// It computes the ratio of busy time to total time elapsed between t1 and t2,
// returning a percentage clamped between 0 and 100.
func calculateBusy(t1, t2 cpu.TimesStat) float64 {
	t1All, t1Busy := getAllBusy(t1)
	t2All, t2Busy := getAllBusy(t2)

	if t2All <= t1All || t2Busy <= t1Busy {
		return 0
	}
	return clampPercent((t2Busy - t1Busy) / (t2All - t1All) * 100)
}

// getCpuPressure reads Linux PSI (Pressure Stall Information) for CPU from
// /proc/pressure/cpu and returns the "some" stall percentages [avg10, avg60, avg300].
// Returns a zero array on non-Linux systems or if the file is unavailable.
func getCpuPressure() [3]float64 {
	if runtime.GOOS != "linux" {
		return [3]float64{}
	}
	f, err := os.Open("/proc/pressure/cpu")
	if err != nil {
		return [3]float64{}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		// Format: "some avg10=X.XX avg60=X.XX avg300=X.XX total=XXXXXX"
		var vals [3]float64
		for _, field := range strings.Fields(line[5:]) {
			kv := strings.SplitN(field, "=", 2)
			if len(kv) != 2 {
				continue
			}
			v, err := strconv.ParseFloat(kv[1], 64)
			if err != nil {
				continue
			}
			switch kv[0] {
			case "avg10":
				vals[0] = v
			case "avg60":
				vals[1] = v
			case "avg300":
				vals[2] = v
			}
		}
		return vals
	}
	return [3]float64{}
}

// getAllBusy calculates the total CPU time and busy CPU time from CPU times statistics.
// On Linux, it excludes guest and guest_nice time from the total to match kernel behavior.
// Returns total CPU time and busy CPU time (total minus idle and I/O wait time).
func getAllBusy(t cpu.TimesStat) (float64, float64) {
	tot := t.Total()
	if runtime.GOOS == "linux" {
		tot -= t.Guest     // Linux 2.6.24+
		tot -= t.GuestNice // Linux 3.2.0+
	}

	busy := tot - t.Idle - t.Iowait

	return tot, busy
}
