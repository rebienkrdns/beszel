package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// readCtxSwitches reads the cumulative context switches count from Linux's
// /proc/stat "ctxt" field. Returns 0 on non-Linux systems.
func readCtxSwitches() uint64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	return readProcStatCounter("/proc/stat", "ctxt")
}

// readInterrupts reads the cumulative hardware interrupts count from Linux's
// /proc/stat "intr" field (first value = total). Returns 0 on non-Linux systems.
func readInterrupts() uint64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	return readProcStatCounter("/proc/stat", "intr")
}

// readProcStatCounter opens path and returns the first numeric value on the
// line that starts with prefix. Used for both "ctxt" and "intr" fields.
func readProcStatCounter(path, prefix string) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		after, ok := strings.CutPrefix(line, prefix+" ")
		if !ok {
			continue
		}
		fields := strings.Fields(after)
		if len(fields) == 0 {
			return 0
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0
		}
		return v
	}
	return 0
}
