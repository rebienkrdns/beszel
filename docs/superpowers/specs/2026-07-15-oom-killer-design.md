# OOM Killer events — design

Date: 2026-07-15
Status: Approved

## Summary

Add host-level OOM Killer event monitoring: a threshold-based alert ("OOM Killer", non-inverted, fires when a new kill is detected) plus a persistent lifetime counter shown in the system info bar. This is the second of the 3-metric sequence agreed with the user (MemAvailable → **OOM Killer** → TCP retransmissions/network errors), following its own spec → plan → implementation cycle. Per the earlier scope decision, this is **host-only** — no per-container OOM tracking (would require Docker events/inspect polling, deferred).

Verified directly in this session's Docker Desktop VM (kernel `6.12.54-linuxkit`): `/proc/vmstat` contains an `oom_kill 0` line — the kernel-side counter this feature reads exists and is accessible in the actual deployment environment, not just in theory.

## Background / established conventions (from MemAvailable, PSI, Battery, disk/network delta tracking)

- Backend: a new `/proc/*` file gets its own small reader file in `agent/` (`psi.go`, `io.go` precedent), Stats/Info struct fields with cbor/json tags, alert switch-case wiring in `alerts_system.go`, `alertInfo` entry in `lib/alerts.ts`.
- **Learned from MemAvailable**: the `alerts.name` PocketBase `select` field in `internal/migrations/0_collections_snapshot_0_19_0_dev_1.go` has a closed enum — any new alert type name MUST be added there or no alert record with that name can ever be created (in tests or production). This is included in the plan from the start this time, not discovered mid-implementation.
- **Learned from MemAvailable**: in `sendSystemAlert`, `isLowAlert(alert.name)` must be evaluated with the alert's raw (un-renamed) name — evaluating it after a display-rename caused a real bug (fixed in the MemAvailable branch, commit `57bc6e39`). This feature's alert is non-inverted (never touches `isLowAlert`), so it isn't at risk of that specific bug, but the subject-text special-case below (see section 4) must still be placed correctly relative to the generic subject/body computation.
- `omitzero`/`omitempty` conventions apply to new fields exactly as established (fixed-size or zero-meaningful fields use `omitzero`; simple non-zero-meaningful counters can use `omitempty`).

## Scope decisions specific to OOM Killer

1. **Host-only** (no per-container OOM tracking) — decided at the start of this feature's brainstorm.
2. **Two separate fields, not one** — the kernel only exposes a monotonically-increasing cumulative counter, but this feature needs two different things from it:
   - **`Stats.OOMKillDelta`** (`uint32`) — new kills since the agent's last poll, computed by the agent (single cached previous value, no per-cache-interval complexity needed since this isn't a rate). Feeds the alert.
   - **`Info.OOMKillCount`** (`uint64`) — the raw cumulative total, copied directly from `/proc/vmstat` each poll (same pattern as `Info.Uptime`). Feeds the persistent info-bar display.
   Using only the cumulative total for alerting would mean the alert can never resolve once triggered (kernel counters only increase, so a static threshold comparison would stay "above threshold" forever until host reboot) — inconsistent with how every other alert in this app self-resolves. Using only the delta for display would make the info-bar counter flicker between 0 and 1 instead of showing a meaningful lifetime total.
3. **Alert mechanism**: reuse the existing non-inverted threshold engine (same as CPU/Memory), not a new event-style mechanism like `Status`. Threshold defaults to `0.5` so any delta `>= 1` fires — the minimum-viable amount of new code, at the cost of the mechanism being a slight semantic mismatch (a "threshold" being used to mean "did this happen at all").
4. **UI**: no dedicated chart (an event counter isn't a trend worth graphing minute-to-minute). Shown as: (a) the alert type in alert configuration, and (b) a lifetime counter in the system info bar, **hidden when zero** (matches the existing info-bar convention of hiding redundant/empty values — a healthy system shows no OOM indicator at all).
5. **Notification wording**: the generic "X above/below threshold" and "averaged X for Y minutes" phrasing doesn't fit an event counter. Special-cased in `sendSystemAlert` (see section 4) to say "OOM Killer event detected" / "OOM Killer events cleared" instead — computed after the generic subject/body (so the generic path is unchanged for every other alert type), overridden only when `alert.name == "OOMKill"`.
6. **Reboot safety**: `/proc/vmstat`'s `oom_kill` resets to 0 on host reboot. The agent's delta computation guards against this (if the new reading is less than the previous cached reading, treat delta as 0 for that poll rather than underflowing to a huge false spike).

## 1. Backend — agent collection

`agent/oom.go` (new):

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

`agent/agent.go` — add to the `Agent` struct (near `diskPrev`/`netIoStats`):

```go
prevOOMKillCount uint64 // Previous cumulative OOM kill count, for delta calculation
```

`agent/system.go` — alongside the existing `a.systemInfo.Uptime, _ = host.Uptime()` line:

```go
oomKillCount := readOOMKillCount()
a.systemInfo.OOMKillCount = oomKillCount
if oomKillCount >= a.prevOOMKillCount {
	systemStats.OOMKillDelta = uint32(oomKillCount - a.prevOOMKillCount)
} else {
	systemStats.OOMKillDelta = 0 // counter reset (host reboot) - avoid underflow/false spike
}
a.prevOOMKillCount = oomKillCount
```

## 2. Data model

`internal/entities/system/system.go` — `Stats` struct, next free cbor key (42):

```go
OOMKillDelta uint32 `json:"okd,omitzero" cbor:"42,keyasint,omitzero"` // OOM kills since last poll
```

`internal/entities/system/system.go` — `Info` struct, next free cbor key (24):

```go
OOMKillCount uint64 `json:"ok,omitempty" cbor:"24,keyasint,omitempty"` // cumulative OOM kills since boot
```

`internal/alerts/alerts.go` — `SystemAlertStats`, mirrored field:

```go
OOMKillDelta uint32 `json:"okd"`
```

`internal/site/src/types.d.ts` — `SystemStats`:

```ts
/** OOM kills since last poll */
okd?: number
```

`internal/site/src/types.d.ts` — `SystemInfo`, alongside `u` (uptime):

```ts
/** cumulative OOM kills since boot */
ok?: number
```

## 3. Records aggregation

`internal/records/records.go`, `AverageSystemStatsSlice`: `OOMKillDelta` is a per-poll count, not a continuous gauge — summing it across an aggregation window (rather than averaging) is the semantically correct behavior (e.g., "3 kills happened across this 10-minute aggregate" is meaningful; "an average of 0.3 kills per poll" is not). Add:

- Accumulation: `sum.OOMKillDelta += stats.OOMKillDelta` (uint32, no divide-by-count for this field — matches how `DiskIO`/`Bandwidth` byte counters are summed-then-divided for rates, but here we deliberately skip the divide step since a sum, not an average, is the meaningful long-interval value).
- No corresponding division line for `OOMKillDelta` (intentional — leave the summed total as-is).

(`Info.OOMKillCount` is not part of `Stats`/`AverageSystemStatsSlice` — it's a live snapshot value on `Info`, not aggregated historically, same as `Uptime`.)

## 4. Alerts

`internal/site/src/lib/alerts.ts` — new entry:

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
```

(`SkullIcon` confirmed available in the installed `lucide-react` version.)

`internal/alerts/alerts_system.go`:
- `HandleSystemAlerts` switch: `case "OOMKill": if data.Stats.OOMKillDelta == 0 { continue }; val = float64(data.Stats.OOMKillDelta); unit = ""`.
- Windowed accumulator switch: `case "OOMKill": alert.val += float64(stats.OOMKillDelta)`.
- No `isLowAlert` change — this is a normal (non-inverted) alert, like CPU/Memory.
- No name-rewrite entry needed for the generic rename block (unlike `MemAvailable`/`Disk`) — the special-cased subject text below doesn't depend on `titleAlertName`.
- **Special-cased subject/body**, added immediately after the existing generic `subject`/`body` computation (so the generic path is byte-identical for every other alert type):

```go
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

`internal/migrations/0_collections_snapshot_0_19_0_dev_1.go`: add `"OOMKill"` to the `alerts.name` select field's `values` enum (required from the start this time — see Background).

## 5. Frontend UI — info bar counter

`internal/site/src/components/routes/system/info-bar.tsx`, in the `systemInfo` memo's `info` array construction, alongside the existing `if (memory) { ... }` block:

```tsx
if (system.info.ok) {
	info.push({
		value: system.info.ok,
		Icon: SkullIcon,
		label: t`OOM Kills`,
	})
}
```

Hidden when `system.info.ok` is `0`/`undefined` (falsy), matching the existing convention for `hostname`/`cpuModel` in this component — a healthy system shows no OOM indicator at all.

## 6. i18n

3 new `t\`...\`` strings: alert name (`OOM Killer`), alert description (`Triggers when the kernel OOM killer terminates a process`), info-bar label (`OOM Kills`). Extracted via lingui to all 30 locales (empty `msgstr`), hand-translated only into Spanish (`es.po`), matching the MemAvailable/Memory/IO Pressure precedent.

## 7. Verification

- Backend: `go build ./agent/... ./internal/entities/... ./internal/alerts/... ./internal/records/... ./internal/migrations/...`; `go test -tags=testing ./agent/... ./internal/entities/... ./internal/records/... ./internal/alerts/...`.
- New tests: an `omitzero`/`omitempty` presence test for `OOMKillDelta`/`OOMKillCount` in `system_test.go` (same shape as the `MemAvailable` one); a records-aggregation test asserting `OOMKillDelta` sums (not averages) across a multi-record slice; an `alerts_system_test.go` case verifying non-inverted trigger/resolve behavior for `OOMKill` (mirroring the `CPU`/`Memory` test shape, not the inverted `Battery`/`MemAvailable` shape); a subject-text regression test asserting the special-cased "OOM Killer event detected"/"OOM Killer events cleared" wording (following the precedent set by `TestMemAvailableAlertSubjectText`, added during the MemAvailable final review, which exists specifically because generic subject-text bugs don't surface in trigger/resolve-state tests).
- Manual: redeploy to the user's local Docker dev stack (`supplemental/docker/same-system/docker-compose.dev.yml`) and confirm: (a) the "OOM Killer" alert type appears in alert configuration with the correct description; (b) the info-bar OOM counter stays hidden at 0 kills (expected steady state); (c) if a kill can be safely simulated in the dev container (e.g., a deliberate `stress`-style OOM inside a disposable test container), confirm the counter appears and the alert fires with the correct event-style subject text — otherwise this sub-check is deferred/skipped with the user's agreement, since deliberately triggering a real OOM kill carries some risk to the dev environment.

## Out of scope

- Per-container OOM tracking (Docker events/inspect polling) — deferred, host-only per the initial scope decision.
- TCP retransmissions/network errors — separate spec, next in the agreed sequence.
- Any dedicated chart/trend visualization for OOM events.
- Any change to the generic (non-`OOMKill`) alert subject/body path in `sendSystemAlert`.
- No new PocketBase collection (only the one enum-value addition to the existing `alerts` collection's `name` field).
