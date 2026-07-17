# TCP Retransmissions / Network Errors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Network Health" chart plus two independent threshold alerts ("TCP Retransmissions", "Network Errors") covering network-degradation signals bandwidth alone doesn't show: TCP retransmitted segments/sec (Linux-only, host-wide, `/proc/net/snmp`) and interface errors+drops/sec (cross-platform, aggregated across public NICs via gopsutil).

**Architecture:** Both new values are computed as **rates** (events/sec), not raw per-poll counts — this lets alerts use the existing default-average windowed path with no special case (verified via the `Bandwidth` alert precedent, which already averages a bytes/sec rate) and keeps the chart's values independent of polling cadence. A new `agent/tcpstats.go` reader parses `/proc/net/snmp`'s `Tcp: RetransSegs` field; interface errors/drops are aggregated inside the existing `sumAndTrackPerNicDeltas` NIC loop in `agent/network.go`. Both new rates are computed inside `updateNetworkStats`, reusing the `msElapsed` already computed there for bandwidth — no duplicate time-tracking.

**Tech Stack:** Go (agent + hub backend, PocketBase), React/TypeScript (Lingui i18n), Bun.

## Global Constraints

- New `alerts.name` values MUST be added to the PocketBase `select` enum in `internal/migrations/0_collections_snapshot_0_19_0_dev_3.go` or no alert record with that name can ever be created (learned the hard way in the MemAvailable/OOM Killer features — included from the start here).
- `isLowAlert` is NOT touched by this feature — both `TCPRetrans` and `NetworkErrors` are normal (non-inverted, "fires above threshold") alerts, same family as CPU/Memory/Bandwidth.
- Both new `Stats` fields (`TCPRetransPs`, `NetworkErrorsPs`) are **averaged**, not summed, in both `records.go`'s long-interval aggregation and the alerts windowed-accumulator finalization — they are continuous rates, not event counts (unlike `OOMKillDelta`). The finalization switch needs **no new case at all** for either — both fall through to the existing `default: alert.val = alert.val / float64(alert.count)`.
- No zero-guard (`if val == 0 { continue }`) in the alert instant-check switch for either field — 0 is a normal, common, healthy value for both, and since they're averaged rates there's no "stuck alert" risk (that risk was specific to `OOMKill`'s raw event-count + sum design, not applicable here).
- No `sendSystemAlert` changes — the generic "above/below threshold, averaged over N minutes" wording is correct for both (continuous rates, unlike `OOMKill`'s discrete-event wording).
- No frontend unit-test framework exists in this repo — frontend tasks are verified via `tsc -b` + `biome check` (scoped to changed files) + a final manual redeploy check.
- Backend tests requiring the `testing` build tag: run with `-tags=testing`.
- i18n: new `t\`...\`` strings extracted to all 30 locales via lingui, hand-translated only into Spanish (`es.po`); other locales keep empty `msgstr`. "TCP" stays untranslated (standard networking term).

---

### Task 1: Entity fields (`TCPRetransPs`, `NetworkErrorsPs`) + tests

**Files:**
- Modify: `internal/entities/system/system.go:58-59` (insert both fields into `Stats`, after `OOMKillDelta`)
- Modify: `internal/entities/system/system_test.go:29,43-44,56` (extend the existing omitzero regression tests)

**Interfaces:**
- Produces: `system.Stats.TCPRetransPs float64` (json `trp`, cbor key `43`) and `system.Stats.NetworkErrorsPs float64` (json `nep`, cbor key `44`) — consumed by Task 2 (agent collection), Task 3 (records aggregation), Task 4 (alerts), Task 6 (frontend chart).

- [ ] **Step 1: Extend the failing tests**

Edit `internal/entities/system/system_test.go`. Update the doc comment and both key lists (both new fields are scalar `omitzero`, same guard as `MemAvailable`/`OOMKillDelta`):

```go
// TestStatsPressureFieldsOmittedWhenZero guards against a regression where
// json:"...,omitempty" was used on fixed-size array fields (encoding/json's
// omitempty never omits a [N]T array, since its length is always N, never
// zero - only omitzero, Go 1.24+, checks the actual zero value). Without
// omitzero, every stats record would serialize memps/mempf/cpup as [0,0,0]
// even when an agent never collected that data (old agents, non-Linux
// hosts), defeating the frontend's presence check that hides the panel.
// MemAvailable (mav), OOMKillDelta (okd), TCPRetransPs (trp), and
// NetworkErrorsPs (nep) are included here too: all are scalar omitzero
// fields with the same "must be absent, not zero, for old agents" requirement.
func TestStatsPressureFieldsOmittedWhenZero(t *testing.T) {
	b, err := json.Marshal(Stats{})
	if err != nil {
		t.Fatalf("marshal zero Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf", "mav", "okd", "trp", "nep"} {
		if _, present := raw[key]; present {
			t.Errorf("expected %q to be omitted for a zero-value Stats, got: %s", key, raw[key])
		}
	}
}

func TestStatsPressureFieldsPresentWhenNonZero(t *testing.T) {
	s := Stats{
		MemPressureSome: [3]float64{1.1, 2.2, 3.3},
		MemPressureFull: [3]float64{4.4, 5.5, 6.6},
		CpuPressure:     [3]float64{7.7, 8.8, 9.9},
		IOPressureSome:  [3]float64{1.2, 3.4, 5.6},
		IOPressureFull:  [3]float64{7.8, 9.0, 1.2},
		MemAvailable:    5.5,
		OOMKillDelta:    1,
		TCPRetransPs:    2.5,
		NetworkErrorsPs: 1.5,
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal populated Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf", "mav", "okd", "trp", "nep"} {
		if _, present := raw[key]; !present {
			t.Errorf("expected %q to be present for a non-zero Stats", key)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/entities/system/... -run TestStatsPressureFields -v`
Expected: FAIL to compile — `unknown field TCPRetransPs in struct literal of type Stats`

- [ ] **Step 3: Add the fields**

Edit `internal/entities/system/system.go`, insert after line 59 (`OOMKillDelta`), inside the `Stats` struct:

```go
	MemAvailable      float64              `json:"mav,omitzero" cbor:"41,keyasint,omitzero"`    // available memory (gb), from /proc/meminfo MemAvailable
	OOMKillDelta      uint32               `json:"okd,omitzero" cbor:"42,keyasint,omitzero"`    // OOM kills since last poll
	TCPRetransPs      float64              `json:"trp,omitzero" cbor:"43,keyasint,omitzero"`    // TCP retransmitted segments/sec (Linux only), /proc/net/snmp Tcp:RetransSegs
	NetworkErrorsPs   float64              `json:"nep,omitzero" cbor:"44,keyasint,omitzero"`    // network interface errors+drops/sec, summed across public interfaces
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags=testing ./internal/entities/system/... -run TestStatsPressureFields -v`
Expected: PASS (both tests)

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/entities/system/system.go
go build ./internal/entities/...
git add internal/entities/system/system.go internal/entities/system/system_test.go
git commit -m "feat(entities): add TCPRetransPs and NetworkErrorsPs fields"
```

---

### Task 2: Agent collection — `/proc/net/snmp` reader + NIC error aggregation + rate computation

**Files:**
- Create: `agent/tcpstats.go`
- Create: `agent/tcpstats_test.go`
- Modify: `agent/agent.go` (add `prevTCPRetransSegs`, `prevNetErrorsTotal` fields + seed them in `NewAgent`)
- Modify: `agent/network.go:153-196` (extend `sumAndTrackPerNicDeltas` return signature; extend `updateNetworkStats`)

**Interfaces:**
- Consumes: `system.Stats.TCPRetransPs`, `system.Stats.NetworkErrorsPs` (Task 1).
- Produces: `readTCPRetransSegs() uint64` — consumed only within this task's own wiring; no other task calls it directly. `sumAndTrackPerNicDeltas`'s new third return value (`totalErrors uint64`) — consumed only by `updateNetworkStats` in this same task.

- [ ] **Step 1: Write the failing test for the `/proc/net/snmp` reader**

Create `agent/tcpstats_test.go`:

```go
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
```

(The reader is split into `readTCPRetransSegs()` — the real entry point, GOOS-gated — and
`readTCPRetransSegsFromFile(path string)` — the actual parser, taking an explicit path so it's
testable with a fixture file on any OS, mirroring how `readPressureFile(path string)` in
`psi.go` already takes an explicit path for the same reason.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./agent/... -run TestReadTCPRetransSegs -v`
Expected: FAIL to compile — `undefined: readTCPRetransSegsFromFile`

- [ ] **Step 3: Create the reader**

Create `agent/tcpstats.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./agent/... -run TestReadTCPRetransSegs -v`
Expected: PASS (all 4 subtests; `TestReadTCPRetransSegs_NonLinux` runs on macOS/CI-non-Linux
runners and skips itself on Linux runners — the other 3 have no GOOS gate and always run)

- [ ] **Step 5: Add agent state + seed it at construction**

Edit `agent/agent.go`, add to the `Agent` struct (near `prevOOMKillCount`):

```go
	prevOOMKillCount          map[uint16]uint64                                     // Previous cumulative OOM kill count per cache interval, for delta calculation
	prevTCPRetransSegs        map[uint16]uint64                                     // Previous cumulative TCP retransmit count per cache interval, for rate calculation
	prevNetErrorsTotal        map[uint16]uint64                                     // Previous cumulative network errors+drops total per cache interval, for rate calculation
```

Edit `agent/agent.go`'s `NewAgent`, alongside the existing `prevOOMKillCount` initialization line:

```go
	agent.prevOOMKillCount = make(map[uint16]uint64)
	agent.prevTCPRetransSegs = make(map[uint16]uint64)
	agent.prevNetErrorsTotal = make(map[uint16]uint64)
```

- [ ] **Step 6: Verify it builds**

Run: `go build ./agent/...`
Expected: no output, exit code 0

- [ ] **Step 7: Commit the reader**

```bash
gofmt -w agent/tcpstats.go agent/tcpstats_test.go agent/agent.go
git add agent/tcpstats.go agent/tcpstats_test.go agent/agent.go
git commit -m "feat(agent): add /proc/net/snmp TCP retransmission reader"
```

- [ ] **Step 8: Extend `sumAndTrackPerNicDeltas` to also aggregate errors+drops**

Edit `agent/network.go` line 153, change the function signature and its two `return` statements:

```go
func (a *Agent) sumAndTrackPerNicDeltas(cacheTimeMs uint16, msElapsed uint64, netIO []psutilNet.IOCountersStat, systemStats *system.Stats) (totalBytesSent, totalBytesRecv, totalErrors uint64) {
	tracker := a.netInterfaceDeltaTrackers[cacheTimeMs]
	if tracker == nil {
		tracker = deltatracker.NewDeltaTracker[string, uint64]()
		a.netInterfaceDeltaTrackers[cacheTimeMs] = tracker
	}
	tracker.Cycle()

	for _, v := range netIO {
		if _, exists := a.netInterfaces[v.Name]; !exists {
			continue
		}
		totalBytesSent += v.BytesSent
		totalBytesRecv += v.BytesRecv
		totalErrors += v.Errin + v.Errout + v.Dropin + v.Dropout

		var upDelta, downDelta uint64
		upKey, downKey := fmt.Sprintf("%sup", v.Name), fmt.Sprintf("%sdown", v.Name)
		tracker.Set(upKey, v.BytesSent)
		tracker.Set(downKey, v.BytesRecv)
		if msElapsed > 0 {
			if prevVal, ok := tracker.Previous(upKey); ok {
				var deltaBytes uint64
				if v.BytesSent >= prevVal {
					deltaBytes = v.BytesSent - prevVal
				} else {
					deltaBytes = v.BytesSent
				}
				upDelta = deltaBytes * 1000 / msElapsed
			}
			if prevVal, ok := tracker.Previous(downKey); ok {
				var deltaBytes uint64
				if v.BytesRecv >= prevVal {
					deltaBytes = v.BytesRecv - prevVal
				} else {
					deltaBytes = v.BytesRecv
				}
				downDelta = deltaBytes * 1000 / msElapsed
			}
		}
		systemStats.NetworkInterfaces[v.Name] = [4]uint64{upDelta, downDelta, v.BytesSent, v.BytesRecv}
	}

	return totalBytesSent, totalBytesRecv, totalErrors
}
```

(Only the `totalErrors` additions and the signature/return-statement changes are new; the
per-NIC bandwidth-delta logic in the middle is unchanged and shown for exact placement.)

- [ ] **Step 9: Wire the two new rate computations into `updateNetworkStats`**

Edit `agent/network.go` line 79, replacing the whole function body:

```go
func (a *Agent) updateNetworkStats(cacheTimeMs uint16, systemStats *system.Stats) {
	// network stats
	a.ensureNetInterfacesInitialized()

	a.ensureNetworkInterfacesMap(systemStats)

	if netIO, err := psutilNet.IOCounters(true); err == nil {
		nis, msElapsed := a.loadAndTickNetBaseline(cacheTimeMs)
		totalBytesSent, totalBytesRecv, totalErrors := a.sumAndTrackPerNicDeltas(cacheTimeMs, msElapsed, netIO, systemStats)
		bytesSentPerSecond, bytesRecvPerSecond := a.computeBytesPerSecond(msElapsed, totalBytesSent, totalBytesRecv, nis)
		a.applyNetworkTotals(cacheTimeMs, netIO, systemStats, nis, totalBytesSent, totalBytesRecv, bytesSentPerSecond, bytesRecvPerSecond)

		if prevErrors, hasPrev := a.prevNetErrorsTotal[cacheTimeMs]; hasPrev && msElapsed > 0 && totalErrors >= prevErrors {
			systemStats.NetworkErrorsPs = float64(totalErrors-prevErrors) * 1000 / float64(msElapsed)
		} else {
			systemStats.NetworkErrorsPs = 0
		}
		a.prevNetErrorsTotal[cacheTimeMs] = totalErrors

		// TCP retransmissions live inside this same block so they can reuse
		// msElapsed from the network baseline above, rather than tracking
		// their own timestamp. If IOCounters fails, TCP-retransmission
		// collection is skipped too for this poll (consistent with every
		// other network stat already being skipped in that case).
		retransSegs := readTCPRetransSegs()
		if prevSegs, hasPrev := a.prevTCPRetransSegs[cacheTimeMs]; hasPrev && msElapsed > 0 && retransSegs >= prevSegs {
			systemStats.TCPRetransPs = float64(retransSegs-prevSegs) * 1000 / float64(msElapsed)
		} else {
			systemStats.TCPRetransPs = 0
		}
		a.prevTCPRetransSegs[cacheTimeMs] = retransSegs
	}
}
```

Both new counters use the same `hasPrev && current >= prev` guard established for
`prevOOMKillCount` (covers first-poll-per-bucket and counter-reset-by-reboot identically), plus
the `msElapsed > 0` guard already used by the existing bandwidth rate calculation on the same
line above (avoids a divide-by-zero-elapsed spike on the very first tick for a bucket).

- [ ] **Step 10: Verify it builds**

Run: `go build ./agent/...`
Expected: no output, exit code 0

- [ ] **Step 11: Run the full agent test suite to check for regressions**

Run: `go test ./agent/... -v 2>&1 | tail -60`
Expected: PASS for all network-related tests (any pre-existing unrelated failures — e.g.
`TestCollectorStartHelpers` or GPU manager tests — were already documented as a pre-existing
baseline in the MemAvailable/OOM Killer features; cross-check against that baseline before
treating a failure as new).

- [ ] **Step 12: gofmt and commit**

```bash
gofmt -w agent/network.go
git add agent/network.go
git commit -m "feat(agent): compute TCP retransmission and network error rates per poll"
```

---

### Task 3: Records aggregation — average both new fields

**Files:**
- Modify: `internal/records/records.go:212` (accumulation loop, after `OOMKillDelta`)
- Modify: `internal/records/records.go:356` (division block, after `MemAvailable`)
- Modify: `internal/records/records_averaging_test.go` (new test)

**Interfaces:**
- Consumes: `system.Stats.TCPRetransPs`, `system.Stats.NetworkErrorsPs` (Task 1).
- Produces: correctly-averaged `TCPRetransPs`/`NetworkErrorsPs` out of `records.AverageSystemStatsSlice`.

- [ ] **Step 1: Write the failing test**

Add to `internal/records/records_averaging_test.go`:

```go
// TestAverageSystemStatsSlice_TCPRetransAndNetworkErrorsAverage verifies that
// TCPRetransPs and NetworkErrorsPs are averaged (not summed) across an
// aggregation window - unlike OOMKillDelta, both are continuous rates
// (events/sec), not per-poll event counts, so "the average rate during this
// window" is the meaningful long-interval value, matching how MemAvailable
// and NetworkSent/NetworkRecv are already averaged.
func TestAverageSystemStatsSlice_TCPRetransAndNetworkErrorsAverage(t *testing.T) {
	input := []system.Stats{
		{TCPRetransPs: 2, NetworkErrorsPs: 1},
		{TCPRetransPs: 4, NetworkErrorsPs: 3},
		{TCPRetransPs: 6, NetworkErrorsPs: 2},
	}

	result := records.AverageSystemStatsSlice(input)

	assert.Equal(t, 4.0, result.TCPRetransPs)
	assert.Equal(t, 2.0, result.NetworkErrorsPs)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/records/... -run TestAverageSystemStatsSlice_TCPRetransAndNetworkErrorsAverage -v`
Expected: FAIL — `result.TCPRetransPs`/`result.NetworkErrorsPs` are both `0` (untouched fields)

- [ ] **Step 3: Add the accumulation**

Edit `internal/records/records.go`, immediately after line 212 (`sum.OOMKillDelta += stats.OOMKillDelta`):

```go
		sum.OOMKillDelta += stats.OOMKillDelta
		sum.TCPRetransPs += stats.TCPRetransPs
		sum.NetworkErrorsPs += stats.NetworkErrorsPs
```

- [ ] **Step 4: Add the division**

Edit `internal/records/records.go`, immediately after line 356 (`sum.MemAvailable = twoDecimals(sum.MemAvailable / count)`):

```go
	sum.MemAvailable = twoDecimals(sum.MemAvailable / count)
	sum.TCPRetransPs = twoDecimals(sum.TCPRetransPs / count)
	sum.NetworkErrorsPs = twoDecimals(sum.NetworkErrorsPs / count)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -tags=testing ./internal/records/... -run TestAverageSystemStatsSlice_TCPRetransAndNetworkErrorsAverage -v`
Expected: PASS

- [ ] **Step 6: Run the full records test suite to check for regressions**

Run: `go test -tags=testing ./internal/records/... -v`
Expected: PASS (all tests, including `TestAverageSystemStatsSlice_MemAvailableAndPSI` and `TestAverageSystemStatsSlice_OOMKillDeltaSums`)

- [ ] **Step 7: gofmt and commit**

```bash
gofmt -w internal/records/records.go
git add internal/records/records.go internal/records/records_averaging_test.go
git commit -m "feat(records): average TCPRetransPs and NetworkErrorsPs across aggregation windows"
```

---

### Task 4: Backend alerting — `TCPRetrans` and `NetworkErrors` thresholds (non-inverted, averaged)

**Files:**
- Modify: `internal/alerts/alerts.go:65` (add both fields to `SystemAlertStats`)
- Modify: `internal/alerts/alerts_system.go:166`, `:377` (2 edit points — no finalization-switch edit needed, see Global Constraints)
- Modify: `internal/migrations/0_collections_snapshot_0_19_0_dev_3.go:84` (add both names to the `alerts.name` enum)
- Modify: `internal/alerts/alerts_system_test.go` (2 new setters + test-table entries)

**Interfaces:**
- Consumes: `system.Stats.TCPRetransPs`, `system.Stats.NetworkErrorsPs` (Task 1).
- Produces: alert names `"TCPRetrans"` and `"NetworkErrors"` recognized end-to-end — consumed by Task 5 (frontend `alertInfo` entries use these exact strings as object keys).

- [ ] **Step 1: Write the failing tests**

Edit `internal/alerts/alerts_system_test.go`. Add setter functions alongside `setOOMKillAlertValue`:

```go
func setTCPRetransAlertValue(info *system.Info, stats *system.Stats, value float64) {
	stats.TCPRetransPs = value
}

func setNetworkErrorsAlertValue(info *system.Info, stats *system.Stats, value float64) {
	stats.NetworkErrorsPs = value
}
```

Add two lines to `TestSystemAlertsOneMin` (after the existing `OOMKill` line):

```go
	testOneMinuteSystemAlert(t, "TCPRetrans", 10, setTCPRetransAlertValue, 12.0, 8.0)
	testOneMinuteSystemAlert(t, "NetworkErrors", 10, setNetworkErrorsAlertValue, 12.0, 8.0)
```

(Both are normal, non-inverted alerts like `CPU`/`Bandwidth` — trigger value `12` is *above* the
`10` threshold, resolve value `8` is *at-or-below* it. With `min == 1`, only the single latest
sample is in the averaging window, so no dilution math applies.)

Add two lines to `TestSystemAlertsTwoMin` (after the existing `OOMKill` line), reusing the shared
multi-minute helper as-is since both fields average exactly like `CPU`/`Bandwidth`:

```go
	testMultiMinuteSystemAlert(t, "TCPRetrans", 10, 2, setTCPRetransAlertValue, 4.0, 12.0, 4.0)
	testMultiMinuteSystemAlert(t, "NetworkErrors", 10, 2, setNetworkErrorsAlertValue, 4.0, 12.0, 4.0)
```

(Same numeric shape as the existing `CPU` row (`testMultiMinuteSystemAlert(t, "CPU", 50, 2,
setCPUAlertValue, 10, 51, 48)`): `testMultiMinuteSystemAlert` submits `baseline`, then `trigger`
twice, then `resolve` once. By the time the "should be untriggered" assertion runs, the
averaging window holds the 2 `trigger` submissions plus the `resolve` submission:
`(12+12+4)/3 = 9.33 <= 10` ✓ resolves; the 2-sample "just triggered" check is
`(12+12)/2 = 12 > 10` ✓ triggers.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=testing ./internal/alerts/... -run TestSystemAlerts -v`
Expected: FAIL — record creation fails first (`name: Invalid value TCPRetrans`) until Step 5
below adds the migration enum entries; after that, FAIL differently (alert never evaluates)
until Step 4 wires the switch cases.

- [ ] **Step 3: Add the `SystemAlertStats` mirror fields**

Edit `internal/alerts/alerts.go`, insert after line 65 (`OOMKillDelta`):

```go
	MemAvailable    float64                       `json:"mav"`
	OOMKillDelta    uint32                        `json:"okd"`
	TCPRetransPs    float64                       `json:"trp"`
	NetworkErrorsPs float64                       `json:"nep"`
}
```

- [ ] **Step 4: Add both alert-evaluation switch cases**

Edit `internal/alerts/alerts_system.go`, insert after line 166-172 (the `OOMKill` case body),
before the switch's closing `}` on line 173:

```go
		case "OOMKill":
			// unlike Battery/MemAvailable, 0 is a normal, common, valid value
			// here (no new OOM kills since last check) - not a "no reading"
			// sentinel - so it must not be skipped, or the alert could never
			// be evaluated back down to untriggered.
			val = float64(data.Stats.OOMKillDelta)
			unit = ""
		case "TCPRetrans":
			val = data.Stats.TCPRetransPs
			unit = "/s"
		case "NetworkErrors":
			val = data.Stats.NetworkErrorsPs
			unit = "/s"
		}
```

Edit `internal/alerts/alerts_system.go`, insert after line 377-378 (`alert.val +=
float64(stats.OOMKillDelta)`), before the `default:` on line 379:

```go
			case "OOMKill":
				alert.val += float64(stats.OOMKillDelta)
			case "TCPRetrans":
				alert.val += stats.TCPRetransPs
			case "NetworkErrors":
				alert.val += stats.NetworkErrorsPs
			default:
				continue
			}
```

No edit to the finalization switch (around line 386-420) — both new alert names are absent from
that switch entirely, so they fall through to the existing `default: alert.val = alert.val /
float64(alert.count)`, exactly like `Bandwidth`. `isLowAlert` is **not** modified — both use the
normal (non-inverted) path.

- [ ] **Step 5: Add the migration enum entries**

Edit `internal/migrations/0_collections_snapshot_0_19_0_dev_3.go`, in the `alerts.name` select
field's `values` array:

```go
					"Battery",
					"MemAvailable",
					"OOMKill",
					"TCPRetrans",
					"NetworkErrors"
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -tags=testing ./internal/alerts/... -run TestSystemAlerts -v`
Expected: PASS (all subtests, including the new `TCPRetrans`/`NetworkErrors` ones and every
pre-existing one)

- [ ] **Step 7: gofmt and commit**

```bash
gofmt -w internal/alerts/alerts.go internal/alerts/alerts_system.go internal/migrations/0_collections_snapshot_0_19_0_dev_3.go
git add internal/alerts/alerts.go internal/alerts/alerts_system.go internal/alerts/alerts_system_test.go internal/migrations/0_collections_snapshot_0_19_0_dev_3.go
git commit -m "feat(alerts): wire TCPRetrans and NetworkErrors threshold alerts"
```

---

### Task 5: Frontend alert definitions

**Files:**
- Modify: `internal/site/src/lib/alerts.ts:2` (add `RefreshCwIcon`, `UnplugIcon` imports)
- Modify: `internal/site/src/lib/alerts.ts:48-54` (insert 2 new `alertInfo` entries after `Bandwidth`)

**Interfaces:**
- Consumes: `RefreshCwIcon`/`UnplugIcon` (new imports, confirmed available as `Icon`-suffixed
  exports in the installed `lucide-react` version), the `AlertInfo` type, backend alert names
  `"TCPRetrans"`/`"NetworkErrors"` (Task 4) as object keys.
- Produces: `alertInfo.TCPRetrans`, `alertInfo.NetworkErrors` — automatically picked up by the
  alert-configuration UI via `Object.keys(alertInfo)`, no separate registration needed.

- [ ] **Step 1: Add the imports**

Edit `internal/site/src/lib/alerts.ts` line 2:

```ts
import { CpuIcon, GaugeIcon, HardDriveIcon, MemoryStickIcon, RefreshCwIcon, ServerIcon, SkullIcon, UnplugIcon } from "lucide-react"
```

- [ ] **Step 2: Add the entries**

Edit `internal/site/src/lib/alerts.ts`, insert immediately after the `Bandwidth` entry (lines 48-54):

```ts
	Bandwidth: {
		name: () => t`Bandwidth`,
		unit: " MB/s",
		icon: EthernetIcon,
		desc: () => t`Triggers when combined up/down exceeds a threshold`,
		max: 250,
	},
	TCPRetrans: {
		name: () => t`TCP Retransmissions`,
		unit: "/s",
		icon: RefreshCwIcon,
		desc: () => t`Triggers when TCP retransmissions exceed a threshold`,
		start: 10,
		min: 1,
		step: 1,
	},
	NetworkErrors: {
		name: () => t`Network Errors`,
		unit: "/s",
		icon: UnplugIcon,
		desc: () => t`Triggers when network interface errors or dropped packets exceed a threshold`,
		start: 10,
		min: 1,
		step: 1,
	},
```

(Only the `TCPRetrans: {...},` and `NetworkErrors: {...},` blocks are new; the `Bandwidth` entry
is shown for exact placement — it must remain unchanged.)

- [ ] **Step 3: Typecheck**

Run: `cd internal/site && bunx tsc -b`
Expected: no errors

- [ ] **Step 4: Lint**

Run: `cd internal/site && bunx biome check src/lib/alerts.ts`
Expected: no errors (biome may auto-fix formatting; if it does, re-stage the file before committing)

- [ ] **Step 5: Commit**

```bash
git add internal/site/src/lib/alerts.ts
git commit -m "feat(alerts-ui): add TCP Retransmissions and Network Errors alert type definitions"
```

---

### Task 6: Frontend — dedicated "Network Health" chart

**Files:**
- Modify: `internal/site/src/types.d.ts:121` (add `trp?: number`, `nep?: number` to `SystemStats`)
- Modify: `internal/site/src/components/routes/system/charts/network-charts.tsx` (new `NetworkHealthChart` component)
- Modify: `internal/site/src/components/routes/system.tsx:11,108,189` (import + register the new component)

**Interfaces:**
- Consumes: `stats.trp`, `stats.nep` (wire fields produced by Task 2, decoded by the existing
  `SystemStats` plumbing — no changes needed there since it's generic), `ChartCard`,
  `AreaChartDefault`, `pinnedAxisDomain` (existing chart primitives already imported in this file).
- Produces: `NetworkHealthChart` component, rendered in the "core" tab's chart grid alongside
  `BandwidthChart`.

- [ ] **Step 1: Add the type fields**

Edit `internal/site/src/types.d.ts`, insert after line 121 (`okd?: number`, in `SystemStats`):

```ts
	/** available memory (gb) */
	mav?: number
	/** OOM kills since last poll */
	okd?: number
	/** TCP retransmitted segments/sec (Linux only) */
	trp?: number
	/** network interface errors+drops/sec */
	nep?: number
```

(Only the `trp?: number` and `nep?: number` lines and their comments are new; the surrounding
lines are shown for exact placement.)

- [ ] **Step 2: Add the chart component**

Edit `internal/site/src/components/routes/system/charts/network-charts.tsx`, insert a new
exported function immediately after `BandwidthChart` (which ends at line 89, before
`export function ContainerNetworkChart`):

```tsx
export function NetworkHealthChart({
	chartData,
	grid,
	dataEmpty,
	showMax,
	isLongerChart,
	maxValues,
}: {
	chartData: ChartData
	grid: boolean
	dataEmpty: boolean
	showMax: boolean
	isLongerChart: boolean
	maxValues: boolean
}) {
	// hardware/OS-dependent presence check, same convention as BatteryChart/
	// TemperatureChart: hide the whole card if the latest record has neither
	// field populated (e.g. a non-Linux agent with no interface errors ever
	// recorded), rather than checking each value individually against 0 -
	// 0 is a normal, healthy reading for both fields.
	const latestStats = chartData.systemStats.at(-1)?.stats
	const showChart = latestStats?.trp !== undefined || latestStats?.nep !== undefined
	if (!showChart) {
		return null
	}

	const maxValSelect = isLongerChart ? <SelectAvgMax max={maxValues} /> : null

	return (
		<ChartCard
			empty={dataEmpty}
			grid={grid}
			title={t`Network Health`}
			cornerEl={maxValSelect}
			description={t`TCP retransmissions and network interface errors`}
		>
			<AreaChartDefault
				chartData={chartData}
				maxToggled={showMax}
				dataPoints={[
					{
						label: t`TCP Retransmissions`,
						dataKey: ({ stats }) => stats?.trp,
						color: 3,
						opacity: 0.3,
					},
					{
						label: t`Errors + Drops`,
						dataKey: ({ stats }) => stats?.nep,
						color: 4,
						opacity: 0.3,
					},
				]}
				tickFormatter={(val) => `${toFixedFloat(val, val >= 10 ? 0 : 1)}/s`}
				contentFormatter={(data) => `${decimalString(data.value)}/s`}
			/>
		</ChartCard>
	)
}
```

- [ ] **Step 3: Register the chart in the system detail page**

Edit `internal/site/src/components/routes/system.tsx` line 11:

```tsx
import { BandwidthChart, ContainerNetworkChart, NetworkHealthChart } from "./system/charts/network-charts"
```

Edit `internal/site/src/components/routes/system.tsx`, both occurrences of `<BandwidthChart
{...coreProps} systemStats={systemStats} />` (lines 108 and 189) — insert immediately after each:

```tsx
					<BandwidthChart {...coreProps} systemStats={systemStats} />

					<NetworkHealthChart {...coreProps} />
```

(Only the `<NetworkHealthChart {...coreProps} />` line is new at each of the two locations; the
`<BandwidthChart>` line is shown for exact placement — it must remain unchanged, and the second
occurrence at line 189 is a different rendering path (the `isLongerChart`/longer-timeframe
layout) that also needs the new chart registered.)

- [ ] **Step 4: Typecheck**

Run: `cd internal/site && bunx tsc -b`
Expected: no errors

- [ ] **Step 5: Lint**

Run: `cd internal/site && bunx biome check src/types.d.ts src/components/routes/system/charts/network-charts.tsx src/components/routes/system.tsx`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/site/src/types.d.ts internal/site/src/components/routes/system/charts/network-charts.tsx internal/site/src/components/routes/system.tsx
git commit -m "feat(ui): add Network Health chart (TCP retransmissions + interface errors)"
```

---

### Task 7: i18n extraction + Spanish translation

**Files:**
- Modify: `internal/site/src/locales/*/*.po` (all 30 locales, auto-generated)
- Modify: `internal/site/src/locales/es/es.po` (hand-translated)

**Interfaces:**
- Consumes: the 6 new `t\`...\`` strings introduced by Tasks 5-6: `TCP Retransmissions` (alert
  name), `Triggers when TCP retransmissions exceed a threshold` (alert desc), `Network Errors`
  (alert name), `Triggers when network interface errors or dropped packets exceed a threshold`
  (alert desc), `Network Health` (chart title), `TCP retransmissions and network interface
  errors` (chart description), `TCP Retransmissions` (chart series label, same string as the
  alert name — lingui will merge duplicate `msgid`s), `Errors + Drops` (chart series label).

- [ ] **Step 1: Extract strings to all locales**

Run: `cd internal/site && bun run sync_no_compile`
Expected: exit code 0; `git status` shows modifications to `src/locales/*/*.po` (new `msgid`
entries with empty `msgstr`) for all 30 locales.

- [ ] **Step 2: Hand-translate Spanish**

Edit `internal/site/src/locales/es/es.po`. Find the new `msgid` entries (added by Step 1's
extraction) and fill in their `msgstr`:

```po
msgid "TCP Retransmissions"
msgstr "Retransmisiones TCP"

msgid "Triggers when TCP retransmissions exceed a threshold"
msgstr "Se activa cuando las retransmisiones TCP superan un umbral"

msgid "Network Errors"
msgstr "Errores de Red"

msgid "Triggers when network interface errors or dropped packets exceed a threshold"
msgstr "Se activa cuando los errores o paquetes descartados de la interfaz de red superan un umbral"

msgid "Network Health"
msgstr "Salud de Red"

msgid "TCP retransmissions and network interface errors"
msgstr "Retransmisiones TCP y errores de interfaz de red"

msgid "Errors + Drops"
msgstr "Errores + Descartes"
```

("TCP" is kept untranslated — a standard, widely-recognized networking term in Spanish-language
sysadmin usage, same convention as leaving "OOM Killer"/"swap"/"kernel" untranslated in `es.po`'s
existing entries.)

- [ ] **Step 3: Compile catalogs**

Run: `cd internal/site && bun run sync`
Expected: exit code 0; compiled `.ts` catalogs updated locally (gitignored, not committed)

- [ ] **Step 4: Commit**

```bash
git add internal/site/src/locales/
git commit -m "i18n: extract TCP Retransmissions/Network Errors strings, add Spanish translations"
```

---

### Task 8: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full backend build**

Run: `go build ./...`
Expected: exit code 0

- [ ] **Step 2: Full backend test suite**

Run: `go test -tags=testing ./...`
Expected: PASS for every package this feature touched (`internal/entities/system`,
`internal/records`, `internal/alerts` when filtered to `-run TestSystemAlerts` as in Task 4,
`agent`). Cross-check any failures outside those against the already-documented pre-existing
baseline failures from the MemAvailable/OOM Killer features before treating them as new.

- [ ] **Step 3: Full frontend typecheck + lint**

Run: `cd internal/site && bunx tsc -b && bunx biome check src/lib/alerts.ts src/types.d.ts src/components/routes/system/charts/network-charts.tsx src/components/routes/system.tsx`
Expected: no errors

- [ ] **Step 4: Manual verification on the local dev stack**

Redeploy using the user's existing dev compose file:

```bash
docker compose -f supplemental/docker/same-system/docker-compose.dev.yml up -d --build
```

Then, in the browser, open a system's detail page and confirm:
1. A "Network Health" chart appears alongside the "Bandwidth" chart, showing two lines (both
   likely flat at/near 0 on a healthy dev system — this is the expected steady state, not a bug).
2. Opening the alert configuration for that system shows two new alert types, "TCP
   Retransmissions" and "Network Errors", each with a `/s`-suffixed numeric threshold input and
   the correct description.
3. Do **not** attempt to deliberately induce packet loss or TCP retransmissions in the shared dev
   environment to test the positive-value path end-to-end — this risks destabilizing network
   connectivity other work depends on. If deeper confidence in the positive path is wanted,
   that's a decision for the user to make explicitly (e.g., `tc netem` in a disposable, isolated
   container), not something to do automatically as part of this verification step.

- [ ] **Step 5: Final commit (if any lint/format fixes were needed)**

```bash
git status --short
```

If clean, no action needed. If any files changed (e.g. biome auto-fixes), stage and commit them
with a `chore: fix lint/format` message.

---

## Out of scope (per spec)

- Per-NIC error/drop breakdown (aggregate total only, matching `Bandwidth`).
- Granular `/proc/net/netstat` `TcpExt:` sub-metrics (`TCPFastRetrans`, `TCPSynRetrans`, etc.) —
  only the single `RetransSegs` total.
- Per-container network-error alerting (no container-level alerting mechanism exists anywhere in
  this codebase today).
- Any info-bar counter for either metric.
- The pre-existing PSI alert migration-enum gap (flagged during MemAvailable, still unfixed) —
  unrelated to this feature, left as a standing follow-up recommendation.
