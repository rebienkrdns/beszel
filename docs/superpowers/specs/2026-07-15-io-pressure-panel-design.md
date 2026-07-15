# IO Pressure (PSI) panel — design

Date: 2026-07-15
Status: Approved

## Summary

Add "IO Pressure (PSI)" (some + full, avg10/avg60/avg300) to the existing Disk I/O "view more" panel, plus 6 new alert types (some/full × avg10/60/300), mirroring the Memory Pressure feature (`docs/superpowers/specs/2026-07-14-memory-pressure-panel-design.md`, implemented in commits `18bd25c0`..`bc735841` plus follow-up fixes through `b9bb6b53`). Unlike CPU Pressure (mirrored in cpu-sheet.tsx) and Memory Pressure (mirrored in memory-sheet.tsx), the Disk I/O chart **already has** a "view more" sheet (`disk-io-sheet.tsx`) — this feature adds a new section to that existing sheet rather than creating a new one.

## Background / established conventions (from CPU/Memory Pressure)

- Backend: agent reads a `/proc/pressure/*` file (Linux only), Stats struct fields with cbor/json tags, `SystemAlertStats` mirror struct, alert switch-case wiring in `alerts_system.go`, `alertInfo` entries in `lib/alerts.ts` with `levels` (Warning >2%/High >5%/Critical >10%) instead of a slider.
- Frontend: shared `pressureColor`/`pressureLabel`/`PressureBadge` in `internal/site/src/components/routes/system/charts/pressure-utils.tsx` (already extracted during Memory Pressure — reused as-is, not modified).
- i18n: extract to all 29 non-English locales (empty `msgstr`, left for Crowdin), hand-translate only Spanish (`es.po`), matching the actual project convention (verified, not assumed).
- **Learned from Memory Pressure**: fixed-size array fields (`[3]float64`) MUST use `json:"...,omitzero"` (not `omitempty`) — Go's `encoding/json` `omitempty` never omits a `[N]T` array regardless of its values (only `omitzero`, Go 1.24+, checks the actual zero value). Apply `omitzero` to `IOPressureSome`/`IOPressureFull`'s JSON tags from the start.

## Scope decisions specific to IO Pressure

1. **Where it renders**: `/proc/pressure/io` is a system-wide kernel signal, not per-device/per-filesystem. `disk-io-sheet.tsx` is reused for both the root filesystem and "extra filesystems" (`extraFsName` prop). The IO Pressure section renders **only when `!extraFsName`** — showing identical system-wide data under every extra-filesystem's panel would be misleading (implies it's specific to that filesystem when it isn't).
2. **Field naming**: avoid the `iops`/`iopf` short-tag pair because "IOPS" (I/O operations per second) is an unrelated, well-established disk-throughput term — a raw-JSON reader could easily misread `iops` as IOPS. Use `iodp`/`iodf` (IO Disk Pressure some/full) instead.
3. **Shared PSI-parsing helper**: this is the third implementation of the same "scan a `/proc/pressure/*` file for `some`/`full` lines" logic (after `agent/cpu.go` and `agent/mem.go`). Extract a shared `agent/psi.go` with `readPressureFile(path string) (some, full [3]float64)` and the existing `parsePressureLine` helper (moved from `mem.go`). Refactor `getCpuPressure()` and `getMemPressure()` to delegate to it — human-approved, since it touches already-shipped `cpu.go`.

## 1. Backend — agent collection

`agent/psi.go` (new):
```go
package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// readPressureFile reads a Linux PSI (Pressure Stall Information) file (e.g.
// /proc/pressure/cpu, /proc/pressure/memory, /proc/pressure/io) and returns
// the "some" and "full" stall percentages [avg10, avg60, avg300] each.
// Returns zero arrays on non-Linux systems, if the file is unavailable, or
// for PSI files that don't have a "full" line (e.g. CPU).
func readPressureFile(path string) (some [3]float64, full [3]float64) {
	if runtime.GOOS != "linux" {
		return
	}
	f, err := os.Open(path)
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
```

`agent/mem.go` shrinks to:
```go
package agent

// getMemPressure reads Linux PSI (Pressure Stall Information) for memory from
// /proc/pressure/memory and returns the "some" and "full" stall percentages
// [avg10, avg60, avg300] each. Returns zero arrays on non-Linux systems or if
// the file is unavailable.
func getMemPressure() (some [3]float64, full [3]float64) {
	return readPressureFile("/proc/pressure/memory")
}
```

`agent/cpu.go`'s `getCpuPressure()` shrinks to:
```go
// getCpuPressure reads Linux PSI (Pressure Stall Information) for CPU from
// /proc/pressure/cpu and returns the "some" stall percentages [avg10, avg60, avg300].
// CPU PSI has no "full" line (meaningless for CPU - if everything is stalled,
// the CPU is idle by definition), so only "some" is returned.
func getCpuPressure() [3]float64 {
	some, _ := readPressureFile("/proc/pressure/cpu")
	return some
}
```
(`cpu.go`'s import block loses `bufio`/`os`/`strconv`/`strings`, no longer used elsewhere in that file.)

`agent/io.go` (new):
```go
package agent

// getIOPressure reads Linux PSI (Pressure Stall Information) for I/O from
// /proc/pressure/io and returns the "some" and "full" stall percentages
// [avg10, avg60, avg300] each. Returns zero arrays on non-Linux systems or if
// the file is unavailable.
func getIOPressure() (some [3]float64, full [3]float64) {
	return readPressureFile("/proc/pressure/io")
}
```

`agent/system.go`: add, after the existing memory-pressure line:
```go
// io pressure (PSI) - Linux only
systemStats.IOPressureSome, systemStats.IOPressureFull = getIOPressure()
```

`agent/mem_test.go`'s `TestParsePressureLine` moves to `agent/psi_test.go` (same test, since `parsePressureLine` now lives in `psi.go`).

## 2. Data model

`internal/entities/system/system.go` — `Stats` struct, next free cbor keys (39, 40):
```go
IOPressureSome [3]float64 `json:"iodp,omitzero" cbor:"39,keyasint,omitzero"` // io PSI some: [avg10, avg60, avg300]
IOPressureFull [3]float64 `json:"iodf,omitzero" cbor:"40,keyasint,omitzero"` // io PSI full: [avg10, avg60, avg300]
```

`internal/alerts/alerts.go` — `SystemAlertStats`, mirrored fields:
```go
IOPressureSome [3]float64 `json:"iodp"`
IOPressureFull [3]float64 `json:"iodf"`
```

`internal/site/src/types.d.ts` — `SystemStats`, alongside `memps`/`mempf`:
```ts
/** io pressure PSI some [avg10, avg60, avg300] (%) */
iodp?: [number, number, number]
/** io pressure PSI full [avg10, avg60, avg300] (%) */
iodf?: [number, number, number]
```

## 3. Frontend components

**`internal/site/src/components/routes/system/charts/io-pressure-chart.tsx`** (new) — exports `IOPressureChart({ chartData, grid, dataEmpty })`, structurally identical to `memory-pressure-chart.tsx`: an internal `IOPressureCard` parameterized by `statsKey: "iodp" | "iodf"`, reusing `PressureBadge` from `./pressure-utils`. Two cards:
- **Some**: title `IO Pressure (PSI) — Some`, description `% of time tasks stalled waiting for I/O — some stall`.
- **Full**: title `IO Pressure (PSI) — Full`, description `% of time tasks stalled waiting for I/O — full stall`.

Top-level component returns `null` only when both `iodp` and `iodf` are absent from the latest stats snapshot; each card independently guards its own key.

**`internal/site/src/components/routes/system/disk-io-sheet.tsx`** (modify) — import `IOPressureChart`, insert `{!extraFsName && <IOPressureChart chartData={chartData} grid={grid} dataEmpty={dataEmpty} />}` immediately after the main throughput `ChartCard` (before `{hasUtilization && <DiskUtilizationChart .../>}`). No other part of this file changes — `DiskUtilizationChart`, the I/O Time/Queue Depth/I/O Await cards, and the `extraFsName`-driven data-fn selection all stay untouched.

No change to `disk-charts.tsx`'s `DiskIOChart` — the sheet (and its "view more" button) is already wired in; this feature only adds content inside it.

## 4. Alerts

`internal/site/src/lib/alerts.ts` — 6 new entries, same shape as `MemPressureSomeAvg10/60/300`/`MemPressureFullAvg10/60/300`:
`IOPressureSomeAvg10`, `IOPressureSomeAvg60`, `IOPressureSomeAvg300`, `IOPressureFullAvg10`, `IOPressureFullAvg60`, `IOPressureFullAvg300` — icon `GaugeIcon`, `levels: [Warning >2%, High >5%, Critical >10%]`.

`internal/alerts/alerts_system.go`:
- `HandleSystemAlerts` switch: 6 new cases reading `data.Stats.IOPressureSome[0|1|2]` / `data.Stats.IOPressureFull[0|1|2]`, `continue` when exactly 0.
- Aggregation loop: same 6 cases, `alert.val += stats.IOPressureSome[...]` / `stats.IOPressureFull[...]`.
- `sendSystemAlert` naming: add `IOPressureSome` → `"IO Pressure Some "` / `IOPressureFull` → `"IO Pressure Full "` rewrites (alongside the existing `CpuPressure`/`MemPressureSome`/`MemPressureFull` rewrites), and extend the title-casing exception to also skip `"IO Pressure"` prefix.

`isLowAlert` unchanged (standard "trigger above threshold" logic).

## 5. i18n

New `t\`...\`` strings (2 panel titles, 2 descriptions, 6 alert names, 6 alert descriptions — 16 total, same count as Memory Pressure) extracted via lingui to all 29 non-English locales (empty `msgstr`), hand-translated only into Spanish (`es.po`), matching the Memory Pressure precedent exactly.

## 6. Verification

- Backend: `go build ./agent/... ./internal/entities/... ./internal/alerts/...`; `go test -tags=testing` for the moved/renamed `psi_test.go` and any new entities test.
- Frontend: `tsc -b`, `biome check` scoped to changed files.
- Manual: redeploy to the user's local Docker dev stack (`supplemental/docker/same-system/docker-compose.dev.yml`, already used for Memory Pressure verification) and confirm the IO Pressure section appears in the Disk I/O "view more" panel (root filesystem only, not extra filesystems), and the 6 new alert types appear in the alert configuration UI with level selectors.

## Out of scope

- No changes to `DiskUtilizationChart`, I/O Time, Queue Depth, or I/O Await sections of `disk-io-sheet.tsx`.
- No new PocketBase collection/migration.
- No behavior change to CPU Pressure or Memory Pressure beyond the `psi.go` extraction (which must be behaviorally identical — verified via task-scoped review, same discipline as the earlier `pressure-utils.tsx` extraction).
