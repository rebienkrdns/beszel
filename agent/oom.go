package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// readOOMKillCount reads the cumulative OOM Killer count from Linux's
// /proc/vmstat "oom_kill" field (kernel 5.19+). Returns 0 on non-Linux
// systems, if the file is unavailable, or if the field is missing
// (older kernels).
func readOOMKillCount() uint64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	f, err := os.Open("/proc/vmstat")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "oom_kill "); ok {
			v, err := strconv.ParseUint(strings.TrimSpace(after), 10, 64)
			if err == nil {
				return v
			}
		}
	}
	return 0
}
