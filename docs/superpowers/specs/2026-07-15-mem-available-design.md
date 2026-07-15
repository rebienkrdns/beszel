# MemAvailable (available memory) — design

Date: 2026-07-15
Status: Approved

## Summary

Add "Available" (MemAvailable, the kernel-computed `/proc/meminfo` `MemAvailable` value) as a new overlay line on the existing "Memory Usage" chart, plus a new threshold-based alert type ("Available Memory", triggers below a GB threshold, inverted logic like `Battery`). This is the first of a 3-metric sequence agreed with the user (MemAvailable → OOM Killer events (host-only) → TCP retransmissions/network errors), each with its own spec → plan → implementation cycle. `r_await`/`w_await` disk latency, originally considered part of this batch, turned out to already be fully implemented (`agent/disk.go`, `disk-io-sheet.tsx`) and is out of scope here.

Also in scope, at the user's explicit request: fix a latent bug in `internal/records/records.go`'s `AverageSystemStatsSlice` where the existing PSI fields (`CpuPressure`, `MemPressureSome`, `MemPressureFull`, `IOPressureSome`, `IOPressureFull`) are never accumulated or averaged, so long-interval aggregated records (10m/20m/1h) show them as zero.

## Background / established conventions (from CPU/Memory/IO Pressure, Battery)

- Backend: `gopsutil`'s `mem.VirtualMemory()` is already called once per collection cycle in `agent/system.go`; its `VirtualMemoryStat.Available` field is the direct equivalent of `/proc/meminfo`'s `MemAvailable` and requires no new syscalls or file parsing.
- Data model: new `Stats` fields use `omitzero` (not `omitempty` — confirmed in the Memory/IO Pressure specs that `omitempty` never omits zero values for non-slice/map types the way `omitzero` does for Go 1.24+), and the next free CBOR `keyasint` is `41`.
- Fields that represent a live "gauge" reading needing historical peak tracking get a `MaxX` sibling (`MaxMem`, `MaxDiskReadPs`, etc.), aggregated in `records.go` via `max(...)`. PSI fields do **not** have `MaxX` siblings — they're simpler single-reading diagnostics. MemAvailable follows the **PSI precedent**, not the Max-tracking one: the "interesting" extremum for available memory would be a *minimum* (a brief dip is the risk signal, not a peak), and the codebase has no min-tracking concept anywhere. Adding one is out of scope — YAGNI.
- Alerts: two backend switch statements in `internal/alerts/alerts_system.go` both require an explicit `case` per metric (both `default: continue`/no special averaging), plus a `SystemAlertStats` mirror struct in `alerts.go`, plus a frontend `alertInfo` entry in `lib/alerts.ts`.
- Inverted ("below threshold") alerts already exist — `Battery` is the working precedent: `invert: true` in the frontend `AlertInfo`, `isLowAlert(name string) bool` in the backend gating both the instant-check and the windowed-check trigger direction the same way `Battery` does.
- Notification subject formatting (`sendSystemAlert` in `alerts_system.go`) rewrites raw switch-case names (`"CpuPressureAvg10"` → `"CPU Pressure Avg10"`, `"Disk"` → `"Disk usage"`) before an implicit lowercase step for the email subject. Any new alert name needs an equivalent rewrite or it renders as an ugly run-together lowercase string (e.g. `"memavailable"`).

## Scope decisions specific to MemAvailable

1. **UI placement**: overlay line in the existing "Memory Usage" chart (`memory-charts.tsx`), not a new card in the Memory Pressure "view more" sheet. Chosen over the sheet option for maximum at-a-glance visibility — decided by the user from 3 presented options (overlay-only / sheet-only / both).
2. **No `Max` variant** (see Background) — single field, no min/max tracking added.
3. **New alert type, yes** — "Available Memory", inverted threshold in GB, mirroring `Battery`'s pattern exactly rather than the 3-tier `levels` pattern used by the PSI alerts (a single GB threshold is the natural unit here, not a % severity ladder).
4. **Records aggregation bug fix** (user-requested, in scope): add the missing accumulate/average lines for the 5 existing PSI fields in `AverageSystemStatsSlice`, scoped strictly to that function — no other change to PSI collection, alerting, or UI behavior.

## 1. Backend — agent collection

`agent/system.go`, immediately after the existing `MemBuffCache`/`MemUsed`/`MemPct` assignment (~line 210-212):

```go
systemStats.MemAvailable = utils.BytesToGigabytes(v.Available)
```

(`v` is the existing `*mem.VirtualMemoryStat` already in scope from the current `mem.VirtualMemory()` call — no new call, no new import.)

## 2. Data model

`internal/entities/system/system.go` — `Stats` struct, next free cbor key (41):

```go
MemAvailable float64 `json:"mav,omitzero" cbor:"41,keyasint,omitzero"` // available memory (gb), from /proc/meminfo MemAvailable
```

`internal/alerts/alerts.go` — `SystemAlertStats`, mirrored field:

```go
MemAvailable float64 `json:"mav"`
```

`internal/site/src/types.d.ts` — alongside `m`/`mu`/`mb`/`mz` (both places these are duplicated, live + historical):

```ts
/** available memory (gb) */
mav?: number
```

## 3. Records aggregation

`internal/records/records.go`, `AverageSystemStatsSlice`:

- Accumulation loop (alongside `sum.MemBuffCache += stats.MemBuffCache`): add `sum.MemAvailable += stats.MemAvailable`.
- Division/finalization block (alongside `sum.MemBuffCache = twoDecimals(sum.MemBuffCache / count)`): add `sum.MemAvailable = twoDecimals(sum.MemAvailable / count)`.

**Bug fix** (existing PSI fields, same function): add the same accumulate + divide pair, currently entirely missing, for:
- `CpuPressure` (`[3]float64` — accumulate/divide each index)
- `MemPressureSome`, `MemPressureFull` (`[3]float64` each)
- `IOPressureSome`, `IOPressureFull` (`[3]float64` each)

No change to any other function or file — this is purely filling in the missing lines in the existing accumulate/divide blocks, following the exact per-index loop style already used for `DiskIoStats` (`for i := range stats.DiskIoStats { sum.DiskIoStats[i] += stats.DiskIoStats[i] }`).

## 4. Frontend chart

`internal/site/src/components/routes/system/charts/memory-charts.tsx`, `MemoryChart`'s `dataPoints` array — new entry, no `stackId` (rendered as an overlay line, same technique as the existing "ZFS ARC" entry):

```tsx
{
  label: t`Available`,
  dataKey: ({ stats }) => stats?.mav,
  color: 1, // --chart-1 (blue) — distinct from the green tones already used by Used/Cache/ZFS ARC
  order: 4, // renders last in the tooltip, separated from the stacked breakdown
},
```

No changes to `tickFormatter`/`contentFormatter` (already generic GB→human-readable conversion via `formatBytes`) or to the chart's `domain` (`[0, totalMem]` already fits GB-denominated available memory).

## 5. Alerts

`internal/site/src/lib/alerts.ts` — new entry:

```ts
MemAvailable: {
  name: () => t`Available Memory`,
  unit: " GB",
  icon: MemoryStickIcon,
  desc: () => t`Triggers when available memory drops below a threshold`,
  invert: true,
  start: 1,
  min: 0.1,
  step: 0.1,
},
```

`internal/alerts/alerts_system.go`:
- `HandleSystemAlerts` switch: `case "MemAvailable": if data.Stats.MemAvailable == 0 { continue }; val = data.Stats.MemAvailable; unit = " GB"` (zero-skip guards against older agent versions that don't send the field, same as the PSI cases).
- Windowed accumulator switch: `case "MemAvailable": alert.val += stats.MemAvailable` (falls through to the existing `default: alert.val = alert.val / float64(alert.count)` for the final average — no special-case needed there).
- `isLowAlert()`: add `|| name == "MemAvailable"`.
- `sendSystemAlert`: add a name-rewrite rule alongside the `Disk`/`LoadAvg`/`CpuPressure`/etc. rewrites: `if alert.name == "MemAvailable" { alert.name = "Available Memory" }`. Not added to the capitalization-exception list — it should lowercase to `"available memory"` for the notification subject, matching how `"Disk"` → `"disk usage"` already behaves.

## 6. i18n

3 new `t\`...\`` strings: chart label (`Available`), alert name (`Available Memory`), alert description (`Triggers when available memory drops below a threshold`). Extracted via lingui to all 29 non-English locales (empty `msgstr`), hand-translated only into Spanish (`es.po`), matching the Memory/IO Pressure precedent.

## 7. Verification

- Backend: `go build ./agent/... ./internal/entities/... ./internal/alerts/... ./internal/records/...`; `go test -tags=testing ./internal/records/... ./internal/alerts/...` (new/updated tests below).
- Frontend: `tsc -b`, `biome check` scoped to changed files.
- Tests: an `omitzero` regression test for `MemAvailable` in `system_test.go` (same shape as the existing `CpuPressure`/`LoadAvg` one); a records-aggregation test asserting `MemAvailable` and the 5 previously-unaggregated PSI fields are correctly averaged across a multi-record slice; an `alerts_system_test.go` case verifying inverted trigger/un-trigger behavior for `MemAvailable`, mirroring the existing `Battery` test.
- Manual: redeploy to the user's local Docker dev stack (`supplemental/docker/same-system/docker-compose.dev.yml`) and confirm the "Available" line renders on the Memory Usage chart, and the "Available Memory" alert type appears in the alert configuration UI with a GB threshold input and "drops below" phrasing.

## Out of scope

- OOM Killer events and TCP retransmissions/network errors — separate specs, next in the agreed sequence.
- `r_await`/`w_await` — already fully implemented, not part of this work.
- Any Min/Max tracking for `MemAvailable`.
- Any change to PSI collection, PSI alerting, or PSI UI beyond the `records.go` aggregation fix described in section 3.
- No new PocketBase collection/migration.
