# OOM Killer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add host-level OOM Killer event monitoring to beszel: a threshold-based alert ("OOM Killer") that fires when the kernel kills a process, plus a lifetime counter shown in the system info bar.

**Architecture:** A new `/proc/vmstat` reader in the agent (`readOOMKillCount`, mirroring the existing `psi.go`/`io.go` reader pattern) feeds two derived values: a per-poll delta (`Stats.OOMKillDelta`, computed by the agent from a cached previous total, feeding the existing non-inverted threshold-alert engine exactly like CPU/Memory) and a raw cumulative total (`Info.OOMKillCount`, copied each poll exactly like `Info.Uptime`, feeding a hide-when-zero info-bar counter). No chart, no per-container tracking (host-only, per the approved spec).

**Tech Stack:** Go (agent + hub backend, PocketBase), React/TypeScript (Lingui i18n), Bun.

## Global Constraints

- New `alerts.name` values MUST be added to the PocketBase `select` enum in `internal/migrations/0_collections_snapshot_0_19_0_dev_1.go` or no alert record with that name can ever be created (learned the hard way in the MemAvailable feature — included from the start here).
- `isLowAlert` is NOT touched by this feature — `OOMKill` is a normal (non-inverted, "fires above threshold") alert, same family as CPU/Memory, not Battery/MemAvailable.
- The generic subject/body computation in `sendSystemAlert` must remain byte-identical for every other alert type — the OOMKill-specific wording is an override applied strictly after the generic computation, gated by `alert.name == "OOMKill"`.
- `Stats.OOMKillDelta` is summed (not averaged) in `records.go`'s long-interval aggregation — an event count across a compacted window is meaningful as a total, not as an average-per-poll.
- No frontend unit-test framework exists in this repo — frontend tasks are verified via `tsc -b` + `biome check` (scoped to changed files, since the whole-project `bun run check` carries ~85 pre-existing unrelated errors) + a final manual redeploy check.
- Backend tests requiring the `testing` build tag: run with `-tags=testing`.
- i18n: new `t\`...\`` strings extracted to all 30 locales via lingui, hand-translated only into Spanish (`es.po`); other locales keep empty `msgstr`.

---

### Task 1: Entity fields (`OOMKillDelta`, `OOMKillCount`) + tests

**Files:**
- Modify: `internal/entities/system/system.go:58-59` (insert `OOMKillDelta` into `Stats`, after `MemAvailable`)
- Modify: `internal/entities/system/system.go:163-164` (insert `OOMKillCount` into `Info`, after `Battery`)
- Modify: `internal/entities/system/system_test.go` (extend the existing omitzero regression tests)

**Interfaces:**
- Produces: `system.Stats.OOMKillDelta uint32` (json `okd`, cbor key `42`) and `system.Info.OOMKillCount uint64` (json `ok`, cbor key `24`) — consumed by Task 2 (agent), Task 3 (records aggregation, `OOMKillDelta` only), Task 4 (alerts, `OOMKillDelta` only).

- [ ] **Step 1: Extend the failing tests**

Edit `internal/entities/system/system_test.go`. Update the doc comment and both key lists (`OOMKillDelta` is another scalar `omitzero` field, same guard as `MemAvailable`):

```go
// TestStatsPressureFieldsOmittedWhenZero guards against a regression where
// json:"...,omitempty" was used on fixed-size array fields (encoding/json's
// omitempty never omits a [N]T array, since its length is always N, never
// zero - only omitzero, Go 1.24+, checks the actual zero value). Without
// omitzero, every stats record would serialize memps/mempf/cpup as [0,0,0]
// even when an agent never collected that data (old agents, non-Linux
// hosts), defeating the frontend's presence check that hides the panel.
// MemAvailable (mav) and OOMKillDelta (okd) are included here too: both are
// scalar omitzero fields with the same "must be absent, not zero, for old
// agents" requirement.
func TestStatsPressureFieldsOmittedWhenZero(t *testing.T) {
	b, err := json.Marshal(Stats{})
	if err != nil {
		t.Fatalf("marshal zero Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf", "mav", "okd"} {
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
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal populated Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf", "mav", "okd"} {
		if _, present := raw[key]; !present {
			t.Errorf("expected %q to be present for a non-zero Stats", key)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/entities/system/... -run TestStatsPressureFields -v`
Expected: FAIL to compile — `unknown field OOMKillDelta in struct literal of type Stats`

- [ ] **Step 3: Add the fields**

Edit `internal/entities/system/system.go`, insert after line 58 (`MemAvailable`), inside the `Stats` struct:

```go
	MemAvailable      float64              `json:"mav,omitzero" cbor:"41,keyasint,omitzero"`    // available memory (gb), from /proc/meminfo MemAvailable
	OOMKillDelta      uint32               `json:"okd,omitzero" cbor:"42,keyasint,omitzero"`    // OOM kills since last poll
```

Edit `internal/entities/system/system.go`, insert after line 163 (`Battery`), inside the `Info` struct:

```go
	Battery        [2]uint8           `json:"bat,omitzero" cbor:"23,keyasint,omitzero"`  // [percent, charge state]
	OOMKillCount   uint64             `json:"ok,omitempty" cbor:"24,keyasint,omitempty"` // cumulative OOM kills since boot
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags=testing ./internal/entities/system/... -run TestStatsPressureFields -v`
Expected: PASS (both tests)

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/entities/system/system.go
go build ./internal/entities/...
git add internal/entities/system/system.go internal/entities/system/system_test.go
git commit -m "feat(entities): add OOMKillDelta and OOMKillCount fields"
```

---

### Task 2: Agent collection — read `/proc/vmstat`, compute delta

**Files:**
- Create: `agent/oom.go`
- Modify: `agent/agent.go` (add `prevOOMKillCount` field + seed it in `NewAgent`)
- Modify: `agent/system.go:269` (alongside the existing `Uptime` line)

**Interfaces:**
- Consumes: `system.Stats.OOMKillDelta`, `system.Info.OOMKillCount` (Task 1).
- Produces: `readOOMKillCount() uint64` — consumed only within this task's own wiring; no other task calls it directly.

No unit test for the `/proc/vmstat` reader itself: matches the established pattern for `readPressureFile`/PSI readers in this codebase (no unit test exists for those either, since they read live OS state that isn't mocked anywhere) — verified via `go build` and the final manual redeploy check (Task 8), which already confirmed in this session that `/proc/vmstat` genuinely has an `oom_kill` line in the actual deployment environment (Docker Desktop's Linux VM, kernel `6.12.54-linuxkit`).

- [ ] **Step 1: Create the reader**

Create `agent/oom.go`:

```go
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
```

- [ ] **Step 2: Add agent state + seed it at construction**

Edit `agent/agent.go`, add to the `Agent` struct (near `diskPrev`/`netIoStats`):

```go
	prevOOMKillCount uint64 // Previous cumulative OOM kill count, for delta calculation
```

Edit `agent/agent.go`'s `NewAgent`, alongside the existing disk/network initialization lines:

```go
	// Initialize disk I/O previous counters storage
	agent.diskPrev = make(map[uint16]map[string]prevDisk)
	// Initialize per-cache-time network tracking structures
	agent.netIoStats = make(map[uint16]system.NetIoStats)
	agent.netInterfaceDeltaTrackers = make(map[uint16]*deltatracker.DeltaTracker[string, uint64])
	// Seed OOM kill baseline from the current cumulative count, so the first
	// collection cycle after startup reports a delta of 0 (not a false spike
	// equal to every OOM kill that happened before the agent even started).
	agent.prevOOMKillCount = readOOMKillCount()
```

(Without this seed step, the very first poll after agent startup would compute `delta = currentTotal - 0`, falsely reporting every historical OOM kill since boot as "new" in that first interval — this is the same class of bug the disk I/O code avoids by seeding `TotalRead`/`TotalWrite` from the first real reading in `initializeDiskIoStats`, rather than from zero.)

- [ ] **Step 3: Wire the collection**

Edit `agent/system.go`, immediately after line 269 (`a.systemInfo.Uptime, _ = host.Uptime()`):

```go
	a.systemInfo.Uptime, _ = host.Uptime()
	oomKillCount := readOOMKillCount()
	a.systemInfo.OOMKillCount = oomKillCount
	if oomKillCount >= a.prevOOMKillCount {
		systemStats.OOMKillDelta = uint32(oomKillCount - a.prevOOMKillCount)
	} else {
		systemStats.OOMKillDelta = 0 // counter reset (host reboot) - avoid underflow/false spike
	}
	a.prevOOMKillCount = oomKillCount
```

(Only the 6 new lines after the `Uptime` assignment are added; the `Uptime` line itself is shown for exact placement — it must remain unchanged.)

- [ ] **Step 4: Verify it builds**

Run: `go build ./agent/...`
Expected: no output, exit code 0

- [ ] **Step 5: Commit**

```bash
git add agent/oom.go agent/agent.go agent/system.go
git commit -m "feat(agent): collect OOM kill count and delta from /proc/vmstat"
```

---

### Task 3: Records aggregation — sum (not average) `OOMKillDelta`

**Files:**
- Modify: `internal/records/records.go:211` (accumulation loop)
- Modify: `internal/records/records_averaging_test.go` (new test)

**Interfaces:**
- Consumes: `system.Stats.OOMKillDelta` (Task 1).
- Produces: correctly-summed `OOMKillDelta` out of `records.AverageSystemStatsSlice`, for any future long-interval consumer (no consumer exists yet — this only affects historical data compaction, not current alert evaluation, which reads raw per-poll records directly).

- [ ] **Step 1: Write the failing test**

Add to `internal/records/records_averaging_test.go`:

```go
// TestAverageSystemStatsSlice_OOMKillDeltaSums verifies that OOMKillDelta is
// summed (not averaged) across an aggregation window. Unlike every other
// numeric field in this function, OOMKillDelta is a per-poll event count, not
// a continuous gauge - "3 kills happened across this 10-minute window" is
// meaningful, "an average of 0.3 kills per poll" is not.
func TestAverageSystemStatsSlice_OOMKillDeltaSums(t *testing.T) {
	input := []system.Stats{
		{OOMKillDelta: 1},
		{OOMKillDelta: 2},
		{OOMKillDelta: 0},
	}

	result := records.AverageSystemStatsSlice(input)

	assert.Equal(t, uint32(3), result.OOMKillDelta)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/records/... -run TestAverageSystemStatsSlice_OOMKillDeltaSums -v`
Expected: FAIL — `result.OOMKillDelta` is `0` (untouched field), not `3`

- [ ] **Step 3: Add the accumulation (no division)**

Edit `internal/records/records.go`, immediately after line 211 (`sum.MemAvailable += stats.MemAvailable`):

```go
		sum.MemAvailable += stats.MemAvailable
		sum.OOMKillDelta += stats.OOMKillDelta
```

Do **not** add a corresponding division line for `OOMKillDelta` in the "Compute averages" block — this is intentional (see the test's doc comment above).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags=testing ./internal/records/... -run TestAverageSystemStatsSlice_OOMKillDeltaSums -v`
Expected: PASS

- [ ] **Step 5: Run the full records test suite to check for regressions**

Run: `go test -tags=testing ./internal/records/... -v`
Expected: PASS (all tests, including `TestAverageSystemStatsSlice_MemAvailableAndPSI` and every other pre-existing test)

- [ ] **Step 6: gofmt and commit**

```bash
gofmt -w internal/records/records.go
git add internal/records/records.go internal/records/records_averaging_test.go
git commit -m "feat(records): sum OOMKillDelta across aggregation windows"
```

---

### Task 4: Backend alerting — `OOMKill` threshold (non-inverted) + event-style notification wording

**Files:**
- Modify: `internal/alerts/alerts.go:64` (add field to `SystemAlertStats`)
- Modify: `internal/alerts/alerts_system.go:165`, `:366`, `:490`, `:83` (4 edit points, see below)
- Modify: `internal/migrations/0_collections_snapshot_0_19_0_dev_1.go:83` (add `"OOMKill"` to the `alerts.name` enum)
- Modify: `internal/alerts/alerts_system_test.go` (new setter + 2 test-table entries + 1 subject-text regression test)

**Interfaces:**
- Consumes: `system.Stats.OOMKillDelta` (Task 1).
- Produces: alert name `"OOMKill"` recognized end-to-end — consumed by Task 5 (frontend `alertInfo` entry uses this exact string as its object key).

- [ ] **Step 1: Write the failing tests**

Edit `internal/alerts/alerts_system_test.go`. Add a setter function alongside `setMemAvailableAlertValue`:

```go
func setOOMKillAlertValue(info *system.Info, stats *system.Stats, value uint32) {
	stats.OOMKillDelta = value
}
```

Add one line to `TestSystemAlertsOneMin` (after the existing `MemAvailable` line):

```go
	testOneMinuteSystemAlert(t, "OOMKill", 0.5, setOOMKillAlertValue, uint32(1), uint32(0))
```

(`OOMKill` is a normal, non-inverted alert like `CPU`/`Memory` — trigger value `1` is *above* the `0.5` threshold, resolve value `0` is *at-or-below* it. With `min == 1`, only the single latest sample is ever in the averaging window, so no dilution math applies here.)

Add one line to `TestSystemAlertsTwoMin` (after the existing `MemAvailable` line):

```go
	testMultiMinuteSystemAlert(t, "OOMKill", 2, 2, setOOMKillAlertValue, uint32(0), uint32(3), uint32(0))
```

**Why threshold `2`, trigger `3`, resolve `0` (not `0.5`/`1`/`0` like the one-minute test)**: `testMultiMinuteSystemAlert` submits `baseline`, then `trigger` twice, then `resolve` once, each ~1 minute apart. Due to how the alert's windowed accumulator filters historical records (`created - 10s < now - min*60s`), by the time the "should be untriggered" assertion runs, the averaging window contains **3** samples — both `trigger` submissions plus the `resolve` submission — not just the `resolve` value alone (confirmed empirically for this exact helper during the MemAvailable feature's Task 4, and independently re-derived here from the existing `CPU` test's own numbers: `testMultiMinuteSystemAlert(t, "CPU", 50, 2, setCPUAlertValue, 10, 51, 48)` only resolves because `(51+51+48)/3 = 50 <= 50`, i.e. resolve equals exactly `3*threshold - 2*trigger`). For `OOMKill`, `trigger` must individually exceed the threshold (for the 2-sample "just triggered" check: `(3+3)/2 = 3 > 2` ✓), and `resolve` must satisfy `(2*trigger + resolve)/3 <= threshold`. With `trigger=3, threshold=2`: `resolve <= 3*2 - 2*3 = 0`, so `resolve = 0` is the only valid non-negative choice (an event-count field can't go negative, unlike `CPU`'s percentage). Using threshold `0.5` here (matching the UI's suggested default) would require a negative resolve value, which `uint32` can't express — this test's threshold/trigger pair exists purely to make the arithmetic land on non-negative integers, and is independent of the `start: 0.5` suggested in `lib/alerts.ts`'s UI config (Task 5).

Add the subject-text regression test, mirroring `TestMemAvailableAlertSubjectText` (place after it):

```go
// TestOOMKillAlertSubjectText guards against the generic "above/below
// threshold" and "averaged X for Y minutes" wording being used for OOMKill,
// which is an event counter, not a continuous value - the event-style
// override in sendSystemAlert must actually take effect.
func TestOOMKillAlertSubjectText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fixture := newSystemAlertTestFixture(t, "OOMKill", 1, 0.5)
		defer fixture.cleanup()

		submitValue(fixture, t, uint32(1), setOOMKillAlertValue)
		waitForSystemAlert(time.Second)

		fixture.assertTriggered(t, true, "Alert should be triggered")
		require.Equal(t, 1, fixture.hub.TestMailer.TotalSend(), "An email should have been sent")
		assert.Contains(t, fixture.hub.TestMailer.LastMessage().Subject, "OOM Killer event detected",
			"OOMKill triggering should use event-style wording, not 'above threshold'")

		submitValue(fixture, t, uint32(0), setOOMKillAlertValue)
		waitForSystemAlert(time.Second)

		fixture.assertTriggered(t, false, "Alert should be untriggered")
		require.Equal(t, 2, fixture.hub.TestMailer.TotalSend(), "A second email should have been sent for untriggering the alert")
		assert.Contains(t, fixture.hub.TestMailer.LastMessage().Subject, "OOM Killer events cleared",
			"OOMKill resolving should use event-style wording, not 'below threshold'")

		waitForSystemAlert(time.Minute)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=testing ./internal/alerts/... -run 'TestSystemAlerts|TestOOMKillAlertSubjectText' -v`
Expected: FAIL — record creation fails first (`name: Invalid value OOMKill`) until Step 5 below adds the migration enum entry; after that, FAIL differently (alert never triggers / generic subject wording) until Steps 3-4 wire the switches and the event-style override.

- [ ] **Step 3: Add the `SystemAlertStats` mirror field**

Edit `internal/alerts/alerts.go`, insert after line 64 (`MemAvailable`):

```go
	MemAvailable    float64                       `json:"mav"`
	OOMKillDelta    uint32                        `json:"okd"`
}
```

- [ ] **Step 4: Add both alert-evaluation switch cases**

Edit `internal/alerts/alerts_system.go`, insert after line 165 (`unit = " GB"`, the `MemAvailable` case body), before the switch's closing `}` on line 166:

```go
		case "MemAvailable":
			if data.Stats.MemAvailable == 0 {
				continue
			}
			val = data.Stats.MemAvailable
			unit = " GB"
		case "OOMKill":
			if data.Stats.OOMKillDelta == 0 {
				continue
			}
			val = float64(data.Stats.OOMKillDelta)
			unit = ""
		}
```

Edit `internal/alerts/alerts_system.go`, insert after line 366 (`alert.val += stats.MemAvailable`), before the `default:` on line 367:

```go
			case "MemAvailable":
				alert.val += stats.MemAvailable
			case "OOMKill":
				alert.val += float64(stats.OOMKillDelta)
			default:
				continue
			}
```

`isLowAlert` is **not** modified — `OOMKill` uses the normal (non-inverted) path, same as `CPU`/`Memory`.

- [ ] **Step 5: Add the migration enum entry**

Edit `internal/migrations/0_collections_snapshot_0_19_0_dev_1.go`, in the `alerts.name` select field's `values` array:

```go
					"Battery",
					"MemAvailable",
					"OOMKill"
```

- [ ] **Step 6: Add the event-style subject/body override**

Edit `internal/alerts/alerts_system.go`, insert immediately after line 490 (`body := fmt.Sprintf(...)`), before the blank line preceding `if err := am.setAlertTriggered(...)`:

```go
	body := fmt.Sprintf("%s averaged %.2f%s for the previous %v %s.", alert.descriptor, alert.val, alert.unit, alert.min, minutesLabel)

	// OOM Killer is an event counter, not a continuous value - "above/below
	// threshold" and "averaged X for Y minutes" don't fit; use event wording.
	if alert.name == "OOMKill" {
		if alert.triggered {
			subject = fmt.Sprintf("%s OOM Killer event detected", systemName)
			body = fmt.Sprintf("The kernel OOM Killer terminated a process on %s.", systemName)
		} else {
			subject = fmt.Sprintf("%s OOM Killer events cleared", systemName)
			body = fmt.Sprintf("No new OOM Killer events on %s in the previous %v %s.", systemName, alert.min, minutesLabel)
		}
	}
```

(Only the `if alert.name == "OOMKill" { ... }` block is new; the `body := fmt.Sprintf(...)` line is shown for exact placement — it must remain unchanged, and this override must come strictly *after* it so every other alert type's generic `subject`/`body` is computed identically to before.)

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test -tags=testing ./internal/alerts/... -run 'TestSystemAlerts|TestOOMKillAlertSubjectText|TestMemAvailableAlertSubjectText' -v`
Expected: PASS (all subtests, including the new `OOMKill` ones and the pre-existing `MemAvailable` ones)

- [ ] **Step 8: Run the full alerts test suite to check for regressions**

Run: `go test -tags=testing ./internal/alerts/... -run TestSystemAlerts -v`
Expected: PASS. (Do not use the bare `./internal/alerts/...` without a `-run` filter — a pre-existing, already-documented goroutine race in `internal/hub/systems.System.setDown` panics when the *entire* package's test suite runs together, reproduced identically on commits that predate this feature; it is unrelated to alert logic and was already investigated during the MemAvailable feature.)

- [ ] **Step 9: gofmt and commit**

```bash
gofmt -w internal/alerts/alerts.go internal/alerts/alerts_system.go internal/migrations/0_collections_snapshot_0_19_0_dev_1.go
git add internal/alerts/alerts.go internal/alerts/alerts_system.go internal/alerts/alerts_system_test.go internal/migrations/0_collections_snapshot_0_19_0_dev_1.go
git commit -m "feat(alerts): wire OOMKill threshold alert with event-style notification wording"
```

---

### Task 5: Frontend alert definition

**Files:**
- Modify: `internal/site/src/lib/alerts.ts:2` (add `SkullIcon` import)
- Modify: `internal/site/src/lib/alerts.ts:270` (insert new `alertInfo` entry before the closing `} as const`)

**Interfaces:**
- Consumes: `SkullIcon` (new import, confirmed available in the installed `lucide-react` version as an alias for `Skull`), the `AlertInfo` type, backend alert name `"OOMKill"` (Task 4) as the object key.
- Produces: `alertInfo.OOMKill` — automatically picked up by the alert-configuration UI via `Object.keys(alertInfo)`, no separate registration needed.

- [ ] **Step 1: Add the import**

Edit `internal/site/src/lib/alerts.ts` line 2:

```ts
import { CpuIcon, GaugeIcon, HardDriveIcon, MemoryStickIcon, ServerIcon, SkullIcon } from "lucide-react"
```

- [ ] **Step 2: Add the entry**

Edit `internal/site/src/lib/alerts.ts`, insert immediately before line 270 (`} as const`):

```ts
	OOMKill: {
		name: () => t`OOM Killer`,
		unit: "",
		icon: SkullIcon,
		desc: () => t`Triggers when the kernel OOM killer terminates a process`,
		start: 0.5,
		min: 0.5,
		step: 1,
	},
} as const
```

(Only the `OOMKill: {...},` block is new; `} as const` is shown for exact placement.)

- [ ] **Step 3: Typecheck**

Run: `cd internal/site && bunx tsc -b`
Expected: no errors

- [ ] **Step 4: Lint**

Run: `cd internal/site && bunx biome check src/lib/alerts.ts`
Expected: no errors (biome may auto-fix formatting; if it does, re-stage the file before committing)

- [ ] **Step 5: Commit**

```bash
git add internal/site/src/lib/alerts.ts
git commit -m "feat(alerts-ui): add OOM Killer alert type definition"
```

---

### Task 6: Frontend — info-bar OOM kill counter

**Files:**
- Modify: `internal/site/src/types.d.ts:54` (add `ok?: number` to `SystemInfo`)
- Modify: `internal/site/src/types.d.ts:117` (add `okd?: number` to `SystemStats`)
- Modify: `internal/site/src/components/routes/system/info-bar.tsx:11` (add `SkullIcon` import)
- Modify: `internal/site/src/components/routes/system/info-bar.tsx:124` (insert new info-bar entry after the `memory` block)

**Interfaces:**
- Consumes: `system.info.ok` (wire field produced by Task 2, decoded by the existing `SystemInfo` plumbing — no changes needed there since it's generic).
- Produces: a visible OOM-kill counter in the info bar, hidden when the count is `0`/`undefined`. `okd?: number` is added to `SystemStats` for type completeness (matching every other `Stats` field having a type entry) even though no UI component reads it directly in this feature (no chart).

- [ ] **Step 1: Add the type fields**

Edit `internal/site/src/types.d.ts`, insert after line 54 (`u: number`, in `SystemInfo`):

```ts
	/** uptime */
	u: number
	/** cumulative OOM kills since boot */
	ok?: number
	/** memory percent */
	mp: number
```

Edit `internal/site/src/types.d.ts`, insert after line 117 (`mav?: number`, in `SystemStats`):

```ts
	/** available memory (gb) */
	mav?: number
	/** OOM kills since last poll */
	okd?: number
	/** swap space (gb) */
	s: number
```

(Only the `ok?: number` and `okd?: number` lines and their comments are new in each block; the surrounding lines are shown for exact placement.)

- [ ] **Step 2: Add the import**

Edit `internal/site/src/components/routes/system/info-bar.tsx`, in the `lucide-react` import block:

```tsx
import {
	AppleIcon,
	ChevronRightSquareIcon,
	ClockArrowUp,
	CpuIcon,
	GlobeIcon,
	MemoryStickIcon,
	MonitorIcon,
	Settings2Icon,
	SkullIcon,
} from "lucide-react"
```

- [ ] **Step 3: Add the info-bar entry**

Edit `internal/site/src/components/routes/system/info-bar.tsx`, immediately after the existing `if (memory) { ... }` block (which ends around line 124):

```tsx
		if (memory) {
			const memValue = formatBytes(memory, false, undefined, false)
			info.push({
				value: `${toFixedFloat(memValue.value, memValue.value >= 10 ? 1 : 2)} ${memValue.unit}`,
				Icon: MemoryStickIcon,
				hide: !memory,
				label: t`Memory`,
			})
		}

		if (system.info.ok) {
			info.push({
				value: system.info.ok,
				Icon: SkullIcon,
				label: t`OOM Kills`,
			})
		}

		return info
```

(Only the new `if (system.info.ok) { ... }` block is added; the `if (memory)` block and the `return info` line are shown for exact placement — they must remain unchanged. No `hide` property is needed on the pushed object itself, since the entry is only pushed at all when `system.info.ok` is truthy — the render loop's existing `if (hide || !value) return null` check also covers this redundantly, which is harmless.)

- [ ] **Step 4: Typecheck**

Run: `cd internal/site && bunx tsc -b`
Expected: no errors

- [ ] **Step 5: Lint**

Run: `cd internal/site && bunx biome check src/types.d.ts src/components/routes/system/info-bar.tsx`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/site/src/types.d.ts internal/site/src/components/routes/system/info-bar.tsx
git commit -m "feat(ui): show OOM kill counter in system info bar"
```

---

### Task 7: i18n extraction + Spanish translation

**Files:**
- Modify: `internal/site/src/locales/*/*.po` (all 30 locales, auto-generated)
- Modify: `internal/site/src/locales/es/es.po` (hand-translated)

**Interfaces:**
- Consumes: the 3 new `t\`...\`` strings introduced by Tasks 5-6: `OOM Killer` (alert name), `Triggers when the kernel OOM killer terminates a process` (alert desc), `OOM Kills` (info-bar label).

- [ ] **Step 1: Extract strings to all locales**

Run: `cd internal/site && bun run sync_no_compile`
Expected: exit code 0; `git status` shows modifications to `src/locales/*/*.po` (new `msgid` entries with empty `msgstr`) for all 30 locales.

- [ ] **Step 2: Hand-translate Spanish**

Edit `internal/site/src/locales/es/es.po`. Find the 3 new `msgid` entries (added by Step 1's extraction) and fill in their `msgstr`:

```po
msgid "OOM Killer"
msgstr "OOM Killer"

msgid "Triggers when the kernel OOM killer terminates a process"
msgstr "Se activa cuando el OOM killer del kernel termina un proceso"

msgid "OOM Kills"
msgstr "OOM Kills"
```

("OOM Killer" and "OOM Kills" are kept untranslated — this is a standard, widely-recognized Linux kernel subsystem name in Spanish-language sysadmin usage, same convention as leaving "swap" or "kernel" untranslated in `es.po`'s existing entries.)

- [ ] **Step 3: Compile catalogs**

Run: `cd internal/site && bun run sync`
Expected: exit code 0; compiled `.ts` catalogs updated locally (gitignored, not committed — confirmed via `git ls-files internal/site/src/locales/ | grep '\.ts$'` returning empty during the MemAvailable feature)

- [ ] **Step 4: Commit**

```bash
git add internal/site/src/locales/
git commit -m "i18n: extract OOM Killer strings, add Spanish translations"
```

---

### Task 8: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full backend build**

Run: `go build ./...`
Expected: exit code 0

- [ ] **Step 2: Full backend test suite**

Run: `go test -tags=testing ./...`
Expected: PASS for every package this feature touched (`internal/entities/system`, `internal/records`, `internal/alerts` when filtered to `-run TestSystemAlerts` as in Task 4, `agent`). Any failures elsewhere should be cross-checked against the already-documented pre-existing baseline failures from the MemAvailable feature (`TestCollectorStartHelpers` + 3 GPU manager tests in `agent`; `TestMultipleSystemsWithSameUniversalToken` + `TestAgentWebSocketIntegration` in `internal/hub`; the `internal/hub/systems.System.setDown` goroutine-race panic when running the whole `internal/alerts` package) — if a *new* failure appears outside those, investigate before proceeding.

- [ ] **Step 3: Full frontend typecheck + lint**

Run: `cd internal/site && bunx tsc -b && bunx biome check src/lib/alerts.ts src/types.d.ts src/components/routes/system/info-bar.tsx`
Expected: no errors

- [ ] **Step 4: Manual verification on the local dev stack**

Redeploy using the user's existing dev compose file:

```bash
docker compose -f supplemental/docker/same-system/docker-compose.dev.yml up -d --build
```

Then, in the browser, open a system's detail page and confirm:
1. Opening the alert configuration for that system shows a new "OOM Killer" alert type with a numeric threshold input (no GB/percent unit) and the correct description.
2. The system info bar shows **no** OOM-kill indicator (expected steady state — the dev container has not had any real OOM kills).
3. Do **not** attempt to deliberately trigger a real OOM kill in the shared dev environment to test the positive-count/alert-firing path end-to-end — this risks destabilizing the Docker Desktop VM other work depends on. If deeper confidence in the positive path is wanted, that's a decision for the user to make explicitly (e.g., a disposable, isolated container), not something to do automatically as part of this verification step.

- [ ] **Step 5: Final commit (if any lint/format fixes were needed)**

```bash
git status --short
```

If clean, no action needed. If any files changed (e.g. biome auto-fixes), stage and commit them with a `chore: fix lint/format` message.

---

## Out of scope (per spec)

- Per-container OOM tracking (Docker events/inspect polling) — deferred, host-only.
- TCP retransmissions/network errors — separate spec, next in the agreed sequence.
- Any dedicated chart/trend visualization for OOM events.
- Any change to the generic (non-`OOMKill`) alert subject/body path in `sendSystemAlert`.
- No new PocketBase collection (only the one enum-value addition to the existing `alerts` collection's `name` field).
