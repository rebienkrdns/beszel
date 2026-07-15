# MemAvailable Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add "Available" (MemAvailable, `/proc/meminfo`'s `MemAvailable`) as a new overlay line on the Memory Usage chart, plus a new "Available Memory" threshold alert (inverted, like `Battery`), and fix a pre-existing bug where PSI fields are never aggregated for long-interval records.

**Architecture:** Follows the exact backend/frontend pattern already established for Memory/IO Pressure and `Battery`: a new `omitzero` field on `system.Stats` populated by the agent from data gopsutil already fetches, mirrored into `SystemAlertStats` for alert evaluation, wired into both alert-evaluation switches in `alerts_system.go`, exposed as a non-stacked overlay series on the existing `MemoryChart`, and registered as a new inverted-logic entry in the frontend `alertInfo` map.

**Tech Stack:** Go (agent + hub backend, `gopsutil/v4`, PocketBase), React/TypeScript (Recharts-based charts, Lingui i18n), Bun.

## Global Constraints

- New `Stats` struct fields use `json:"...,omitzero"` (never `omitempty` — `omitempty` does not omit non-zero-length fixed types the way `omitzero` does; already the established rule from the Memory/IO Pressure specs).
- No `Max`/`Min` variant for `MemAvailable` — out of scope (see spec `docs/superpowers/specs/2026-07-15-mem-available-design.md`, "Scope decisions").
- Follow the `Battery` inverted-alert pattern exactly for `MemAvailable` (`invert: true` frontend, `isLowAlert` backend).
- i18n: new `t\`...\`` strings extracted to all locales via lingui, hand-translated only into Spanish (`es.po`); other locales keep empty `msgstr`.
- Backend tests requiring the `testing` build tag: run with `-tags=testing`.
- No frontend unit-test framework exists in this repo (no `vitest`/`jest`, no `*.test.tsx` files) — frontend tasks are verified via `tsc -b` + `biome check` + the final manual redeploy check, matching existing project convention. Do not introduce a new test framework.

---

### Task 1: `MemAvailable` entity field + omitzero regression tests

**Files:**
- Modify: `internal/entities/system/system.go:51-53` (insert new field after `IOPressureFull`)
- Modify: `internal/entities/system/system_test.go` (extend existing omitzero regression tests)

**Interfaces:**
- Produces: `system.Stats.MemAvailable float64` (json tag `mav`, cbor key `41`) — consumed by Task 2 (agent), Task 3 (records aggregation), Task 4 (alerts).

- [ ] **Step 1: Extend the failing tests**

Edit `internal/entities/system/system_test.go`. Update the doc comment and both key lists (this test already guards the `omitzero`-vs-`omitempty` regression for fixed-size fields; `MemAvailable` is a plain `float64` so it exercises the same guard for scalar fields):

```go
// TestStatsPressureFieldsOmittedWhenZero guards against a regression where
// json:"...,omitempty" was used on fixed-size array fields (encoding/json's
// omitempty never omits a [N]T array, since its length is always N, never
// zero - only omitzero, Go 1.24+, checks the actual zero value). Without
// omitzero, every stats record would serialize memps/mempf/cpup as [0,0,0]
// even when an agent never collected that data (old agents, non-Linux
// hosts), defeating the frontend's presence check that hides the panel.
// MemAvailable (mav) is included here too: it's a scalar omitzero field with
// the same "must be absent, not zero, for old agents" requirement.
func TestStatsPressureFieldsOmittedWhenZero(t *testing.T) {
	b, err := json.Marshal(Stats{})
	if err != nil {
		t.Fatalf("marshal zero Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf", "mav"} {
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
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal populated Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf", "mav"} {
		if _, present := raw[key]; !present {
			t.Errorf("expected %q to be present for a non-zero Stats", key)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/entities/system/... -run TestStatsPressureFields -v`
Expected: FAIL to compile — `unknown field MemAvailable in struct literal of type Stats`

- [ ] **Step 3: Add the field**

Edit `internal/entities/system/system.go`, insert after line 52 (`MaxDiskIoStats`) — actually insert immediately after the `IOPressureFull` field so it's grouped with the other recently-added metrics:

```go
	IOPressureFull    [3]float64           `json:"iodf,omitzero" cbor:"40,keyasint,omitzero"`  // io PSI full: [avg10, avg60, avg300]
	MemAvailable      float64              `json:"mav,omitzero" cbor:"41,keyasint,omitzero"`   // available memory (gb), from /proc/meminfo MemAvailable
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags=testing ./internal/entities/system/... -run TestStatsPressureFields -v`
Expected: PASS (both `TestStatsPressureFieldsOmittedWhenZero` and `TestStatsPressureFieldsPresentWhenNonZero`)

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/entities/system/system.go
go build ./internal/entities/...
git add internal/entities/system/system.go internal/entities/system/system_test.go
git commit -m "feat(entities): add MemAvailable field to Stats"
```

---

### Task 2: Agent collection — wire `MemAvailable` from gopsutil

**Files:**
- Modify: `agent/system.go:212` (inside the existing `mem.VirtualMemory()` block)

**Interfaces:**
- Consumes: `system.Stats.MemAvailable` (Task 1), `v *mem.VirtualMemoryStat` (already in scope from the existing `mem.VirtualMemory()` call at `agent/system.go:180`), `utils.BytesToGigabytes` (already imported/used in this file).
- Produces: `systemStats.MemAvailable` populated on every collection cycle on all platforms gopsutil supports.

No new unit test: this file has no existing per-metric unit tests for `MemUsed`/`MemPct`/`MemBuffCache`/`MemZfsArc` either (gopsutil's live OS calls aren't mocked anywhere in this codebase) — verified via `go build` here and the manual redeploy check in Task 8, matching the established pattern for this file.

- [ ] **Step 1: Add the assignment**

Edit `agent/system.go`, immediately after line 212:

```go
		systemStats.Mem = utils.BytesToGigabytes(v.Total)
		systemStats.MemBuffCache = utils.BytesToGigabytes(cacheBuff)
		systemStats.MemUsed = utils.BytesToGigabytes(v.Used)
		systemStats.MemPct = utils.TwoDecimals(v.UsedPercent)
		systemStats.MemAvailable = utils.BytesToGigabytes(v.Available)
	}
```

(Only the new `systemStats.MemAvailable = ...` line is added; the surrounding lines are shown for exact placement — they must remain unchanged.)

- [ ] **Step 2: Verify it builds**

Run: `go build ./agent/...`
Expected: no output, exit code 0

- [ ] **Step 3: Commit**

```bash
git add agent/system.go
git commit -m "feat(agent): collect MemAvailable from gopsutil VirtualMemory"
```

---

### Task 3: Records aggregation — `MemAvailable` + PSI aggregation bug fix

**Files:**
- Modify: `internal/records/records.go:210` (accumulation loop), `internal/records/records.go:346` (division block)
- Modify: `internal/records/records_averaging_test.go` (new test)

**Interfaces:**
- Consumes: `system.Stats.MemAvailable`, `.CpuPressure`, `.MemPressureSome`, `.MemPressureFull`, `.IOPressureSome`, `.IOPressureFull` (all `[3]float64` except `MemAvailable`).
- Produces: correctly-averaged values for all 6 fields out of `records.AverageSystemStatsSlice`.

This task also fixes a pre-existing bug (user-requested, see spec "Scope decisions" #4): `CpuPressure`, `MemPressureSome`, `MemPressureFull`, `IOPressureSome`, `IOPressureFull` are currently never accumulated or divided in `AverageSystemStatsSlice`, so aggregated long-interval records (10m/20m/1h) always show them as zero.

- [ ] **Step 1: Write the failing test**

Add to `internal/records/records_averaging_test.go`:

```go
func TestAverageSystemStatsSlice_MemAvailableAndPSI(t *testing.T) {
	input := []system.Stats{
		{
			MemAvailable:    4.0,
			CpuPressure:     [3]float64{1.0, 2.0, 3.0},
			MemPressureSome: [3]float64{4.0, 5.0, 6.0},
			MemPressureFull: [3]float64{7.0, 8.0, 9.0},
			IOPressureSome:  [3]float64{10.0, 11.0, 12.0},
			IOPressureFull:  [3]float64{13.0, 14.0, 15.0},
		},
		{
			MemAvailable:    2.0,
			CpuPressure:     [3]float64{3.0, 4.0, 5.0},
			MemPressureSome: [3]float64{6.0, 7.0, 8.0},
			MemPressureFull: [3]float64{9.0, 10.0, 11.0},
			IOPressureSome:  [3]float64{12.0, 13.0, 14.0},
			IOPressureFull:  [3]float64{15.0, 16.0, 17.0},
		},
	}

	result := records.AverageSystemStatsSlice(input)

	assert.Equal(t, 3.0, result.MemAvailable)
	assert.Equal(t, [3]float64{2.0, 3.0, 4.0}, result.CpuPressure)
	assert.Equal(t, [3]float64{5.0, 6.0, 7.0}, result.MemPressureSome)
	assert.Equal(t, [3]float64{8.0, 9.0, 10.0}, result.MemPressureFull)
	assert.Equal(t, [3]float64{11.0, 12.0, 13.0}, result.IOPressureSome)
	assert.Equal(t, [3]float64{14.0, 15.0, 16.0}, result.IOPressureFull)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/records/... -run TestAverageSystemStatsSlice_MemAvailableAndPSI -v`
Expected: FAIL — all assertions fail (each field currently averages to its zero value: `0.0` / `[3]float64{0,0,0}`)

- [ ] **Step 3: Fix the accumulation loop**

Edit `internal/records/records.go`, immediately after line 210 (`sum.MemZfsArc += stats.MemZfsArc`):

```go
		sum.MemZfsArc += stats.MemZfsArc
		sum.MemAvailable += stats.MemAvailable
		for i := range stats.CpuPressure {
			sum.CpuPressure[i] += stats.CpuPressure[i]
			sum.MemPressureSome[i] += stats.MemPressureSome[i]
			sum.MemPressureFull[i] += stats.MemPressureFull[i]
			sum.IOPressureSome[i] += stats.IOPressureSome[i]
			sum.IOPressureFull[i] += stats.IOPressureFull[i]
		}
```

- [ ] **Step 4: Fix the division block**

Edit `internal/records/records.go`, immediately after line 346 (`sum.MemZfsArc = twoDecimals(sum.MemZfsArc / count)`):

```go
	sum.MemZfsArc = twoDecimals(sum.MemZfsArc / count)
	sum.MemAvailable = twoDecimals(sum.MemAvailable / count)
	for i := range sum.CpuPressure {
		sum.CpuPressure[i] = twoDecimals(sum.CpuPressure[i] / count)
		sum.MemPressureSome[i] = twoDecimals(sum.MemPressureSome[i] / count)
		sum.MemPressureFull[i] = twoDecimals(sum.MemPressureFull[i] / count)
		sum.IOPressureSome[i] = twoDecimals(sum.IOPressureSome[i] / count)
		sum.IOPressureFull[i] = twoDecimals(sum.IOPressureFull[i] / count)
	}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -tags=testing ./internal/records/... -run TestAverageSystemStatsSlice_MemAvailableAndPSI -v`
Expected: PASS

- [ ] **Step 6: Run the full records test suite to check for regressions**

Run: `go test -tags=testing ./internal/records/... -v`
Expected: PASS (all tests, including the pre-existing `TestAverageSystemStatsSlice_BasicAveraging`, `TestAverageSystemStatsSlice_PeakValues`, etc.)

- [ ] **Step 7: gofmt and commit**

```bash
gofmt -w internal/records/records.go
git add internal/records/records.go internal/records/records_averaging_test.go
git commit -m "fix(records): aggregate MemAvailable and PSI fields in AverageSystemStatsSlice

PSI fields (CpuPressure, MemPressureSome/Full, IOPressureSome/Full) were
never accumulated or divided in AverageSystemStatsSlice, so aggregated
long-interval records (10m/20m/1h) always showed them as zero."
```

---

### Task 4: Backend alerting — `MemAvailable` threshold (inverted)

**Files:**
- Modify: `internal/alerts/alerts.go:63` (add field to `SystemAlertStats`)
- Modify: `internal/alerts/alerts_system.go:159`, `:358`, `:446`, `:495` (4 edit points, see below)
- Modify: `internal/alerts/alerts_system_test.go` (new setter + 2 test-table entries)

**Interfaces:**
- Consumes: `system.Stats.MemAvailable` (Task 1).
- Produces: alert name `"MemAvailable"` recognized end-to-end (instant check, windowed check, inverted trigger logic, notification subject formatting) — consumed by Task 5 (frontend `alertInfo` entry uses this exact string as its object key).

- [ ] **Step 1: Write the failing tests**

Edit `internal/alerts/alerts_system_test.go`. Add a setter function alongside `setBatteryAlertValue`:

```go
func setMemAvailableAlertValue(info *system.Info, stats *system.Stats, value float64) {
	stats.MemAvailable = value
}
```

Add one line to `TestSystemAlertsOneMin` (after the existing `Battery` line):

```go
	testOneMinuteSystemAlert(t, "MemAvailable", 4, setMemAvailableAlertValue, 3.9, 4.1)
```

Add one line to `TestSystemAlertsTwoMin` (after the existing `Battery` line):

```go
	testMultiMinuteSystemAlert(t, "MemAvailable", 4, 2, setMemAvailableAlertValue, 10, 3.9, 4.1)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=testing ./internal/alerts/... -run TestSystemAlerts -v`
Expected: FAIL to compile — `stats.MemAvailable undefined (type *system.Stats has no field or method MemAvailable)` is already fixed by Task 1, so instead: the `MemAvailable` sub-tests time out / never trigger, because `HandleSystemAlerts` has no `case "MemAvailable"` yet and `isLowAlert` doesn't know about it — expect `FAIL: Alert should be triggered` (the alert never triggers because the switch's `default` implicitly `continue`s past it in the windowed path, and the instant-check switch has no matching case so `val` stays `0`).

- [ ] **Step 3: Add the `SystemAlertStats` mirror field**

Edit `internal/alerts/alerts.go`, insert after line 63 (`IOPressureFull`):

```go
	IOPressureFull  [3]float64                    `json:"iodf"`
	MemAvailable    float64                        `json:"mav"`
}
```

- [ ] **Step 4: Add the instant-check switch case**

Edit `internal/alerts/alerts_system.go`, insert after line 159 (the last `IOPressureFullAvg300` case body, before the switch's closing `}` on line 160):

```go
		case "IOPressureFullAvg300":
			if data.Stats.IOPressureFull[2] == 0 {
				continue
			}
			val = data.Stats.IOPressureFull[2]
			unit = "%"
		case "MemAvailable":
			if data.Stats.MemAvailable == 0 {
				continue
			}
			val = data.Stats.MemAvailable
			unit = " GB"
		}
```

- [ ] **Step 5: Add the windowed-accumulator switch case**

Edit `internal/alerts/alerts_system.go`, insert after line 358 (`case "IOPressureFullAvg300": alert.val += stats.IOPressureFull[2]`), before the `default:` on line 359:

```go
				case "IOPressureFullAvg300":
					alert.val += stats.IOPressureFull[2]
				case "MemAvailable":
					alert.val += stats.MemAvailable
				default:
					continue
				}
```

- [ ] **Step 6: Add the notification name rewrite**

Edit `internal/alerts/alerts_system.go`, insert after line 447 (the closing `}` of the IOPressure rename block), before the blank line at 448:

```go
	// format IOPressure names
	if after, ok := strings.CutPrefix(alert.name, "IOPressureSome"); ok {
		alert.name = "IO Pressure Some " + after
	} else if after, ok := strings.CutPrefix(alert.name, "IOPressureFull"); ok {
		alert.name = "IO Pressure Full " + after
	}
	// format MemAvailable name
	if alert.name == "MemAvailable" {
		alert.name = "Available Memory"
	}
```

(Not added to the capitalization-exception list on line 451-452 — it should lowercase to `"available memory"` for the notification subject, matching how `"Disk"` → `"disk usage"` already behaves.)

- [ ] **Step 7: Update `isLowAlert`**

Edit `internal/alerts/alerts_system.go` line 494-496:

```go
func isLowAlert(name string) bool {
	return name == "Battery" || name == "MemAvailable"
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test -tags=testing ./internal/alerts/... -run TestSystemAlerts -v`
Expected: PASS (all sub-tests, including the new `MemAvailable` ones)

- [ ] **Step 9: Run the full alerts test suite to check for regressions**

Run: `go test -tags=testing ./internal/alerts/... -v`
Expected: PASS

- [ ] **Step 10: gofmt and commit**

```bash
gofmt -w internal/alerts/alerts.go internal/alerts/alerts_system.go
git add internal/alerts/alerts.go internal/alerts/alerts_system.go internal/alerts/alerts_system_test.go
git commit -m "feat(alerts): wire MemAvailable threshold alert (inverted, like Battery)"
```

---

### Task 5: Frontend alert definition

**Files:**
- Modify: `internal/site/src/lib/alerts.ts:259` (insert new `alertInfo` entry before the closing `} as const`)

**Interfaces:**
- Consumes: `MemoryStickIcon` (already imported in this file, used by the existing `Memory` entry), the `AlertInfo` type (`internal/site/src/types.d.ts:339`), backend alert name `"MemAvailable"` (Task 4) as the object key.
- Produces: `alertInfo.MemAvailable` — automatically picked up by the alert-configuration UI via `Object.keys(alertInfo)` (`alerts-sheet.tsx:29`), no separate registration needed.

- [ ] **Step 1: Add the entry**

Edit `internal/site/src/lib/alerts.ts`, insert immediately before line 260 (`} as const`):

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
} as const
```

(Only the `MemAvailable: {...},` block is new; `} as const` is shown for exact placement.)

- [ ] **Step 2: Typecheck**

Run: `cd internal/site && bunx tsc -b`
Expected: no errors

- [ ] **Step 3: Lint**

Run: `cd internal/site && bun run check`
Expected: no errors (biome may auto-fix formatting; if it does, re-stage the file before committing)

- [ ] **Step 4: Commit**

```bash
git add internal/site/src/lib/alerts.ts
git commit -m "feat(alerts-ui): add Available Memory alert type definition"
```

---

### Task 6: Frontend chart — "Available" overlay line on Memory Usage

**Files:**
- Modify: `internal/site/src/types.d.ts:115` (add `mav?: number` field)
- Modify: `internal/site/src/components/routes/system/charts/memory-charts.tsx:81` (add new `dataPoints` entry)

**Interfaces:**
- Consumes: `stats.mav` (wire field produced by Task 1/2, decoded by the existing `SystemStatsRecord`/`ChartData` plumbing — no changes needed there since it's generic).
- Produces: a visible "Available" line on the `MemoryChart` component, no new components or props.

- [ ] **Step 1: Add the type field**

Edit `internal/site/src/types.d.ts`, insert after line 115 (`mz?: number`):

```ts
	/** zfs arc memory (gb) */
	mz?: number
	/** available memory (gb) */
	mav?: number
	/** swap space (gb) */
	s: number
```

(Only the `mav?: number` line and its comment are new.)

- [ ] **Step 2: Add the chart data point**

Edit `internal/site/src/components/routes/system/charts/memory-charts.tsx`, insert after the `Cache / Buffers` entry (after line 81, before the closing `]}` of the `dataPoints` array):

```tsx
				dataPoints={[
					{
						label: t`Used`,
						dataKey: ({ stats }) => (showMax ? stats?.mm : stats?.mu),
						color: 2,
						opacity: 0.4,
						stackId: "1",
						order: 3,
					},
					{
						label: "ZFS ARC",
						dataKey: ({ stats }) => (showMax ? null : stats?.mz),
						color: "hsla(175 60% 45% / 0.8)",
						opacity: 0.5,
						order: 2,
					},
					{
						label: t`Cache / Buffers`,
						dataKey: ({ stats }) => (showMax ? null : stats?.mb),
						color: "hsla(160 60% 45% / 0.5)",
						opacity: 0.4,
						stackId: "1",
						order: 1,
					},
					{
						label: t`Available`,
						dataKey: ({ stats }) => stats?.mav,
						color: 1,
						order: 4,
					},
				]}
```

(Only the final `{ label: t\`Available\`, ... }` object is new; the rest of the array is shown for exact placement — it must remain unchanged.)

- [ ] **Step 3: Typecheck**

Run: `cd internal/site && bunx tsc -b`
Expected: no errors

- [ ] **Step 4: Lint**

Run: `cd internal/site && bun run check`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/site/src/types.d.ts internal/site/src/components/routes/system/charts/memory-charts.tsx
git commit -m "feat(ui): add Available Memory overlay line to Memory Usage chart"
```

---

### Task 7: i18n extraction + Spanish translation

**Files:**
- Modify: `internal/site/src/locales/*/​*.po` (all 30 locales, auto-generated)
- Modify: `internal/site/src/locales/es/es.po` (hand-translated)

**Interfaces:**
- Consumes: the 3 new `t\`...\`` strings introduced by Tasks 5-6: `Available Memory` (alert name), `Triggers when available memory drops below a threshold` (alert desc), `Available` (chart label).

- [ ] **Step 1: Extract strings to all locales**

Run: `cd internal/site && bun run sync_no_compile`
Expected: exit code 0; `git status` shows modifications to `src/locales/*/*.po` (new `msgid` entries with empty `msgstr`) for all 30 locales, and updated `.ts` catalogs for locales that were already compiled.

- [ ] **Step 2: Hand-translate Spanish**

Edit `internal/site/src/locales/es/es.po`. Find the 3 new `msgid` entries (added by Step 1's extraction) and fill in their `msgstr`:

```po
msgid "Available Memory"
msgstr "Memoria disponible"

msgid "Triggers when available memory drops below a threshold"
msgstr "Se activa cuando la memoria disponible cae por debajo de un umbral"

msgid "Available"
msgstr "Disponible"
```

- [ ] **Step 3: Compile catalogs**

Run: `cd internal/site && bun run sync`
Expected: exit code 0; compiled `.ts` catalogs updated, `es.ts` includes the 3 new translated strings

- [ ] **Step 4: Commit**

```bash
git add internal/site/src/locales/
git commit -m "i18n: extract Available Memory strings, add Spanish translations"
```

---

### Task 8: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full backend build**

Run: `go build ./...`
Expected: exit code 0

- [ ] **Step 2: Full backend test suite**

Run: `go test -tags=testing ./...`
Expected: PASS (all packages, including `internal/entities/system`, `internal/records`, `internal/alerts`)

- [ ] **Step 3: Full frontend typecheck + lint**

Run: `cd internal/site && bunx tsc -b && bun run check`
Expected: no errors

- [ ] **Step 4: Manual verification on the local dev stack**

Redeploy using the user's existing dev compose file:

```bash
docker compose -f supplemental/docker/same-system/docker-compose.dev.yml up -d --build
```

Then, in the browser, open a system's detail page and confirm:
1. The "Memory Usage" chart shows a new "Available" line (distinct color from Used/Cache/ZFS ARC), and its tooltip shows a plausible GB value below the total.
2. Opening the alert configuration for that system shows a new "Available Memory" alert type with a GB threshold input, and its description reads "Average drops below X GB" (inverted phrasing, like `Battery`).

- [ ] **Step 5: Final commit (if any lint/format fixes were needed)**

```bash
git status --short
```

If clean, no action needed. If any files changed (e.g. biome auto-fixes), stage and commit them with a `chore: fix lint/format` message.

---

## Out of scope (per spec)

- OOM Killer events and TCP retransmissions/network errors — separate specs, next in the agreed sequence.
- Any Min/Max tracking for `MemAvailable`.
- Any change to PSI collection or PSI UI beyond the `records.go` aggregation fix in Task 3.
- No new PocketBase collection/migration.
