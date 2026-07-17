package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// readTCPRetransSegs reads the cumulative count of retransmitted TCP segments
// from Linux's /proc/net/snmp ("Tcp:" line, "RetransSegs" field - the same
// counter `netstat -s` reports as "segments retransmited"). Returns 0 on
// non-Linux systems.
func readTCPRetransSegs() uint64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	return readTCPRetransSegsFromFile("/proc/net/snmp")
}

// readTCPRetransSegsFromFile parses the "Tcp:" header/value line pair from a
// /proc/net/snmp-formatted file at path, returning the value at the same
// column index as the "RetransSegs" field name in the header line. Returns 0
// if the file is unavailable or the field is missing.
func readTCPRetransSegsFromFile(path string) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	fieldIndex := -1
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Tcp:") {
			continue
		}
		fields := strings.Fields(line)
		if fieldIndex == -1 {
			// this is the header line - find RetransSegs's column position
			for i, name := range fields {
				if name == "RetransSegs" {
					fieldIndex = i
					break
				}
			}
			if fieldIndex == -1 {
				return 0 // field not present in this kernel's header
			}
			continue
		}
		// this is the values line - read the same column position
		if fieldIndex >= len(fields) {
			return 0
		}
		v, err := strconv.ParseUint(fields[fieldIndex], 10, 64)
		if err != nil {
			return 0
		}
		return v
	}
	return 0
}
