package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const sampleProcNetSnmp = `Ip: Forwarding DefaultTTL InReceives InHdrErrors InAddrErrors ForwDatagrams InUnknownProtos InDiscards InDelivers OutRequests OutDiscards OutNoRoutes ReasmTimeout ReasmReqds ReasmOKs ReasmFails FragOKs FragFails FragCreates
Ip: 2 64 123456 0 0 0 0 0 123000 100000 0 0 0 0 0 0 0 0 0
Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs InErrs OutRsts InCsumErrors RetransSegs
Tcp: 1 200 120000 -1 1234 567 12 34 56 123456 123456 12 34 0 789
Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors
Udp: 100 0 0 100 0 0
`

func writeTempProcNetSnmp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "snmp")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestReadTCPRetransSegsFromFile(t *testing.T) {
	// readTCPRetransSegsFromFile has no GOOS gate itself (only its caller,
	// readTCPRetransSegs, does) - it's pure file parsing and runs on any OS.
	path := writeTempProcNetSnmp(t, sampleProcNetSnmp)
	got := readTCPRetransSegsFromFile(path)
	if got != 789 {
		t.Errorf("expected RetransSegs = 789, got %d", got)
	}
}

func TestReadTCPRetransSegsFromFile_MissingField(t *testing.T) {
	content := `Tcp: RtoAlgorithm RtoMin
Tcp: 1 200
`
	path := writeTempProcNetSnmp(t, content)
	got := readTCPRetransSegsFromFile(path)
	if got != 0 {
		t.Errorf("expected 0 for missing RetransSegs field, got %d", got)
	}
}

func TestReadTCPRetransSegsFromFile_MissingFile(t *testing.T) {
	got := readTCPRetransSegsFromFile("/nonexistent/path/snmp")
	if got != 0 {
		t.Errorf("expected 0 for missing file, got %d", got)
	}
}

func TestReadTCPRetransSegs_NonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this case only applies on non-Linux systems")
	}
	got := readTCPRetransSegs()
	if got != 0 {
		t.Errorf("expected 0 on non-Linux GOOS, got %d", got)
	}
}
