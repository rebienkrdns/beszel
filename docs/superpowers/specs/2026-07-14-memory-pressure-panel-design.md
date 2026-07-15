# Memory Pressure (PSI) panel — design

Date: 2026-07-14
Status: Approved

## Summary

Add a "view more" button to the Memory Usage chart (mirroring the existing button on the CPU Usage chart) that opens a side panel showing **Memory Pressure (PSI)**, with both `some` and `full` stall metrics (avg10/avg60/avg300 each). This requires new backend PSI collection (Linux `/proc/pressure/memory` currently isn't read anywhere in the codebase — only CPU pressure exists today), new data model fields, new frontend chart/panel components, and six new alert types (some/full × avg10/60/300), following the pattern established by the recently-shipped CPU Pressure feature (`37b63e89`).

## Background / existing pattern (CPU Pressure)

- `agent/cpu.go:122-164` `getCpuPressure()` reads `/proc/pressure/cpu` (Linux only, `some` line only — CPU PSI has no `full` line at the kernel level), returns `[avg10, avg60, avg300]`.
- `agent/system.go:161` calls it: `systemStats.CpuPressure = getCpuPressure()`.
- `internal/entities/system/system.go:53`: `CpuPressure [3]float64 \`json:"cpup,omitempty" cbor:"36,keyasint,omitzero"\`` on the `Stats` struct (persisted as a serialized blob per `system_stats` record — no PocketBase schema/migration needed for new stat fields).
- `internal/alerts/alerts.go:59`: mirrored field `CpuPressure [3]float64 \`json:"cpup"\`` on `SystemAlertStats`.
- `internal/site/src/types.d.ts:92-93`: `cpup?: [number, number, number]` on `SystemStats`.
- `internal/site/src/components/routes/system/charts/cpu-pressure-chart.tsx`: `CpuPressureChart` — a `Card` with 3 color-coded `PressureBadge`s (avg10/60/300, thresholds <1/<2/<5/<10/else → Excellent/Normal/Warning/High/Critical) plus a `LineChartDefault` historical trend. Returns `null` when no pressure data.
- `internal/site/src/components/routes/system/cpu-sheet.tsx`: `CpuCoresSheet` — a `Sheet`/`SheetTrigger` "view more" button (`MoreHorizontalIcon`, `title={t\`View more\`}`, `variant="outline" size="icon"`) that lazily mounts `ChartTimeSelect` + `CpuPressureChart` + other CPU breakdown cards. Gated behind an agent-version check because it also shows per-core breakdown (not applicable to memory).
- `internal/site/src/components/routes/system/disk-io-sheet.tsx`: same "view more" button pattern, **without** a version gate — closer template for the memory sheet, which has no version-dependent sub-features.
- `internal/site/src/components/routes/system/charts/cpu-charts.tsx:35-40`: the button is wired into `CpuChart`'s `cornerEl`.
- `internal/site/src/lib/alerts.ts:95-127`: `CpuPressureAvg10/60/300` entries in `alertInfo`, using `levels` (predefined `Select` dropdown: Warning >2%, High >5%, Critical >10%) instead of a numeric slider.
- `internal/alerts/alerts_system.go`: `HandleSystemAlerts` (per-check switch, lines ~70-87) and the aggregation loop (lines ~257-262) both read `data.Stats.CpuPressure[0|1|2]` keyed by alert name string; `sendSystemAlert` (lines 324-383) reformats `CpuPressureAvg10` → `"CPU Pressure Avg10"` and special-cases the `"CPU Pressure"` prefix to avoid lowercasing the alert title.
- i18n: Lingui `t\`...\`` macro strings, extracted via lingui's extract command into per-locale `.po` files (`internal/site/src/locales/{locale}/{locale}.po`); English source string is the `msgid` itself. 29 locales configured besides `en` (`ar, bg, cs, da, de, es, fa, fr, he, hr, hu, id, it, ja, ko, nl, no, pl, pt, tr, ru, sl, sr, sv, uk, vi, zh, zh-CN, zh-HK`).

Currently there is **no** memory pressure code anywhere in the repo (confirmed via exhaustive grep) — this is new backend + frontend work, not just a frontend wiring task.

## Scope

1. Agent: real PSI collection for memory (`some` **and** `full`, unlike CPU which only has `some`).
2. Data model: new fields end-to-end (agent → entity → alerts struct → frontend type).
3. Frontend: "view more" button on the Memory Usage chart, opening a panel with two stacked cards — Memory Pressure (PSI) Some, and Memory Pressure (PSI) Full — each with avg10/60/300 badges and a historical line chart.
4. Alerts: 6 new alert types (`MemPressureSomeAvg10/60/300`, `MemPressureFullAvg10/60/300`), predefined-level selector, same thresholds as CPU pressure (>2%/>5%/>10%).
5. i18n: new strings translated into all 29 supported locales (not just Spanish).

## 1. Backend — agent collection

New file `agent/mem.go`:

```go
// getMemPressure reads Linux PSI (Pressure Stall Information) for memory from
// /proc/pressure/memory and returns the "some" and "full" stall percentages
// [avg10, avg60, avg300] each. Returns zero arrays on non-Linux systems or if
// the file is unavailable.
func getMemPressure() (some [3]float64, full [3]float64)
```

Same parsing approach as `getCpuPressure` (`agent/cpu.go:122-164`), but reads both the `some ` and `full ` prefixed lines from `/proc/pressure/memory` instead of only `some `.

Wired into `agent/system.go`, immediately after the existing CPU pressure line (~161):

```go
// memory pressure (PSI) - Linux only
systemStats.MemPressureSome, systemStats.MemPressureFull = getMemPressure()
```

## 2. Data model

`internal/entities/system/system.go` — `Stats` struct, next available cbor keys (37, 38):

```go
MemPressureSome [3]float64 `json:"memps,omitempty" cbor:"37,keyasint,omitzero"` // PSI some: [avg10, avg60, avg300]
MemPressureFull [3]float64 `json:"mempf,omitempty" cbor:"38,keyasint,omitzero"` // PSI full: [avg10, avg60, avg300]
```

`internal/alerts/alerts.go` — `SystemAlertStats` struct, mirrored fields:

```go
MemPressureSome [3]float64 `json:"memps"`
MemPressureFull [3]float64 `json:"mempf"`
```

`internal/site/src/types.d.ts` — `SystemStats` interface, alongside `cpup`:

```ts
/** memory pressure PSI some [avg10, avg60, avg300] (%) */
memps?: [number, number, number]
/** memory pressure PSI full [avg10, avg60, avg300] (%) */
mempf?: [number, number, number]
```

No PocketBase migration needed — `stats` is a serialized blob field, not individual columns.

## 3. Frontend components

**Shared helper extraction**: pull `pressureColor`, `pressureLabel`, and `PressureBadge` out of `cpu-pressure-chart.tsx` into a shared module (e.g. `internal/site/src/components/routes/system/charts/pressure-utils.tsx`) so both CPU and Memory pressure charts use the same badge/color/label logic instead of duplicating it. `cpu-pressure-chart.tsx` is updated to import from the new shared module.

**`internal/site/src/components/routes/system/charts/memory-pressure-chart.tsx`** — exports `MemoryPressureChart({ chartData, grid, dataEmpty })`. Returns `null` if neither `memps` nor `mempf` is present on the latest stats snapshot. Otherwise renders two stacked `Card`s (same visual structure as `CpuPressureChart`, one per stall type):

- **Some**: title `Memory Pressure (PSI) — Some`, description `% of time at least one task stalled waiting for memory`, badges avg10/60/300 from `stats?.memps`, `LineChartDefault` with 3 series reading `stats?.memps?.[0|1|2]`.
- **Full**: title `Memory Pressure (PSI) — Full`, description `% of time all tasks stalled waiting for memory`, badges avg10/60/300 from `stats?.mempf`, `LineChartDefault` with 3 series reading `stats?.mempf?.[0|1|2]`.

Each card independently checks its own data presence (e.g. skip rendering the Full card if `mempf` absent, for forward/backward compatibility with agents that might report only one).

**`internal/site/src/components/routes/system/memory-sheet.tsx`** — new file, default export `MemorySheet`, modeled on `disk-io-sheet.tsx` (no agent-version gate, unlike `cpu-sheet.tsx`, since there's no per-core-style breakdown feature involved). Structure:

```tsx
<Sheet open={open} onOpenChange={setOpen}>
  <DialogTitle className="sr-only">{t`Memory Usage`}</DialogTitle>
  <SheetTrigger asChild>
    <Button title={t`View more`} variant="outline" size="icon" className="shrink-0 max-sm:absolute max-sm:top-0 max-sm:end-0">
      <MoreHorizontalIcon />
    </Button>
  </SheetTrigger>
  {hasOpened.current && (
    <SheetContent aria-describedby={undefined} className="overflow-auto w-200 !max-w-full p-4 sm:p-6">
      <ChartTimeSelect className="w-[calc(100%-2em)] bg-card" agentVersion={chartData.agentVersion} />
      <MemoryPressureChart chartData={chartData} grid={grid} dataEmpty={dataEmpty} />
    </SheetContent>
  )}
</Sheet>
```

**`internal/site/src/components/routes/system/charts/memory-charts.tsx`** — in `MemoryChart`, add `<MemorySheet chartData={chartData} dataEmpty={dataEmpty} grid={grid} />` into `cornerEl`, alongside the existing `maxValSelect` (same pattern as `cpu-charts.tsx:35-40`).

## 4. Alerts

**`internal/site/src/lib/alerts.ts`** — 6 new entries in `alertInfo`, same shape as `CpuPressureAvg10/60/300` (icon `GaugeIcon`, `levels: [Warning >2%, High >5%, Critical >10%]`):

- `MemPressureSomeAvg10` / `MemPressureSomeAvg60` / `MemPressureSomeAvg300`
- `MemPressureFullAvg10` / `MemPressureFullAvg60` / `MemPressureFullAvg300`

**`internal/alerts/alerts_system.go`**:

- `HandleSystemAlerts` switch: add cases for all 6 names, reading `data.Stats.MemPressureSome[0|1|2]` / `data.Stats.MemPressureFull[0|1|2]`, `continue` when value is exactly 0 (same "no data" convention as CPU pressure).
- Aggregation loop: same 6 cases, `alert.val += stats.MemPressureSome[...]` / `stats.MemPressureFull[...]`.
- `sendSystemAlert` name formatting: add prefix rewrites `MemPressureSome` → `"Memory Pressure Some "` and `MemPressureFull` → `"Memory Pressure Full "` (alongside the existing `CpuPressure` → `"CPU Pressure "` rewrite).
- Title-casing exception: extend the lowercase-skip condition to also skip when `strings.HasPrefix(titleAlertName, "Memory Pressure")`.

`isLowAlert` is unchanged — memory pressure alerts use standard "trigger above threshold" logic, same as CPU pressure.

## 5. i18n

New `t\`...\`` strings introduced by this feature (panel titles/descriptions for Some/Full, alert names/descriptions) are extracted via the project's lingui extract command, which adds the new `msgid`s (with empty `msgstr`) to all 29 non-English `.po` files automatically.

**Correction from initial design**: inspection of the actual `.po` files shows that, despite the `a2082ddc` commit message ("add Spanish translations"), only the **Spanish** locale has real translated `msgstr` values for the CPU Pressure strings — the other 28 locales were left with `msgstr ""` after extraction. Each `.po` also carries `X-Crowdin-Project`/`X-Crowdin-File` headers, confirming non-Spanish translations are sourced from Crowdin (an external translation platform), not hand-written in this repo. Memory Pressure follows the same established pattern: extract to all locales (leaving 28 empty for Crowdin to pick up), and hand-translate only Spanish (`es.po`), matching exactly what was done for CPU Pressure.

## 6. Verification

- Backend: `go build ./...`; if `agent/cpu.go`'s `getCpuPressure` has unit tests, add an equivalent for `getMemPressure` covering the `some`/`full` parsing.
- Frontend: TypeScript typecheck; manual browser verification — open a system page, click "view more" on the Memory Usage chart, confirm both Some and Full cards render with badges + line charts populated from real or mocked data; confirm the button/panel behave gracefully (no crash, no empty panel) when PSI data is absent (e.g. non-Linux agent).
- Alerts: in the alert configuration panel, confirm all 6 new Memory Pressure alert types appear with a level `Select` (not a slider), matching the CPU Pressure alert UI.

## Out of scope

- No "full" metric for CPU (kernel doesn't expose it — unchanged).
- No new PocketBase collection/migration.
- No changes to the CPU pressure alert behavior beyond extracting shared badge/color/label helpers.
