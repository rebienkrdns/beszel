package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// getMemPressure reads Linux PSI (Pressure Stall Information) for memory from
// /proc/pressure/memory and returns the "some" and "full" stall percentages
// [avg10, avg60, avg300] each. Returns zero arrays on non-Linux systems or if
// the file is unavailable.
func getMemPressure() (some [3]float64, full [3]float64) {
	if runtime.GOOS != "linux" {
		return
	}
	f, err := os.Open("/proc/pressure/memory")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "some "):
			some = parsePressureLine(line[5:])
		case strings.HasPrefix(line, "full "):
			full = parsePressureLine(line[5:])
		}
	}
	return
}

// parsePressureLine parses the avg10/avg60/avg300 fields from the portion of
// a PSI line after the "some "/"full " prefix has been stripped, e.g.
// "avg10=0.00 avg60=0.00 avg300=0.00 total=0".
func parsePressureLine(fields string) [3]float64 {
	var vals [3]float64
	for _, field := range strings.Fields(fields) {
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
