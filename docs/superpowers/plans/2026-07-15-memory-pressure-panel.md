# Memory Pressure (PSI) Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "view more" button to the Memory Usage chart that opens a panel showing Memory Pressure (PSI) — both `some` and `full` stall metrics — mirroring the CPU Pressure feature shipped in commit `37b63e89`.

**Architecture:** New Linux-only PSI collection in the agent (`/proc/pressure/memory`, both `some` and `full` lines) flows through the existing `Stats` blob (no new PocketBase collection) to a new frontend "view more" sheet on the Memory Usage chart, with two stacked pressure cards (Some/Full) reusing shared badge/color/label helpers extracted from the CPU pressure chart. Six new alert types are wired the same way CPU pressure alerts are. New UI strings are extracted for all 29 locales but only hand-translated into Spanish, matching this repo's actual (Crowdin-based) i18n workflow.

**Tech Stack:** Go 1.26 (agent, hub, entities, alerts), React 19 + TypeScript + Recharts + Lingui (frontend), PocketBase (storage, unchanged).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-14-memory-pressure-panel-design.md` — every requirement in that spec must map to a task below.
- Backend `Stats` struct cbor keys: next free integers are `37` and `38` (highest used is `36`, `CpuPressure`). Use exactly `37` for `MemPressureSome`, `38` for `MemPressureFull`.
- JSON field tags: `memps` (some), `mempf` (full) — used consistently across `internal/entities/system/system.go`, `internal/alerts/alerts.go`, and `internal/site/src/types.d.ts`.
- Alert type name strings (used as map keys end-to-end, frontend `alertInfo` and backend switch statements): `MemPressureSomeAvg10`, `MemPressureSomeAvg60`, `MemPressureSomeAvg300`, `MemPressureFullAvg10`, `MemPressureFullAvg60`, `MemPressureFullAvg300`. These exact strings must match byte-for-byte between `internal/site/src/lib/alerts.ts` and `internal/alerts/alerts_system.go`.
- No PocketBase migration is needed anywhere in this plan — `stats` is a serialized blob field.
- **Environment note:** `go` was not preinstalled in this dev environment; it has been installed via `brew install go` (1.26.5) for this session. Backend verification commands below assume `go` is on `PATH` (add `export PATH="/opt/homebrew/bin:$PATH"` if a fresh shell doesn't have it).
- **Environment note:** `internal/site/embed.go` requires `internal/site/dist` to exist (via `//go:embed all:dist`) for ANY package that transitively imports `internal/site` (this includes `internal/alerts`) to build/test. If `internal/site/dist` doesn't exist, create a placeholder exactly as the project's own `Makefile` `dev-hub`/`build-hub-dev` targets do: `mkdir -p internal/site/dist && touch internal/site/dist/index.html`. This is a pre-existing project quirk, not something introduced by this plan.
- **Environment note:** `go test -tags=testing ./agent/...` has 7 pre-existing failures in this dev environment, all under `TestCollectorStartHelpers`/`TestNewGPUManagerPriority*` (GPU collector tests that depend on host binaries like `nvidia-smi` not present on this machine). These are unrelated to this feature — do not try to fix them. Confirm new work doesn't add failures beyond this known baseline.
- **Environment note:** `go test -tags=testing ./internal/alerts/...` panics intermittently with `invalid memory address or nil pointer dereference` when running the full package (confirmed pre-existing, reproduces before any change in this plan, unrelated to status/goroutine teardown races in existing tests — not to be fixed here). Prefer running specific test names with `-run` to verify targeted behavior instead of the full package run.
- Frontend package manager: this repo's canonical lockfile is `internal/site/bun.lock` (tracked in git). `bun` is not installed in this dev environment; `pnpm` is available as a substitute for local verification (`pnpm --dir internal/site install`). **Never stage or commit `internal/site/pnpm-lock.yaml` or `internal/site/node_modules`** if pnpm generates them.
- Frontend has no unit test runner configured (no `test` script in `package.json`). Verification for frontend tasks is: TypeScript build (`tsc -b`, `noEmit: true`), Biome lint/check, and manual browser verification (dev server) — not unit tests.
- i18n scope (confirmed against the actual `.po` files, correcting the original spec draft): only `internal/site/src/locales/es/es.po` gets hand-written translations. The other 28 locales get the new `msgid`s added with empty `msgstr` via `lingui extract` (left for Crowdin, matching exactly what happened for CPU Pressure — verified only `es.po` has real CPU Pressure translations, the rest are empty).

---

## File Structure

**New files:**
- `agent/mem.go` — `getMemPressure()` + `parsePressureLine()` (Linux PSI collection for memory)
- `agent/mem_test.go` — unit tests for `parsePressureLine`
- `internal/site/src/components/routes/system/charts/pressure-utils.tsx` — shared `pressureColor`, `pressureLabel`, `PressureBadge` (extracted from `cpu-pressure-chart.tsx`)
- `internal/site/src/components/routes/system/charts/memory-pressure-chart.tsx` — `MemoryPressureChart` (Some + Full cards)
- `internal/site/src/components/routes/system/memory-sheet.tsx` — `MemorySheet` ("view more" button + panel)

**Modified files:**
- `agent/system.go` — wire `getMemPressure()` into stats collection
- `internal/entities/system/system.go` — add `MemPressureSome`/`MemPressureFull` to `Stats`
- `internal/alerts/alerts.go` — add same fields to `SystemAlertStats`
- `internal/alerts/alerts_system.go` — add 6 alert switch cases (×2 places) + name formatting
- `internal/site/src/types.d.ts` — add `memps`/`mempf` to `SystemStats`
- `internal/site/src/lib/alerts.ts` — add 6 `alertInfo` entries
- `internal/site/src/components/routes/system/charts/cpu-pressure-chart.tsx` — use shared `pressure-utils.tsx`
- `internal/site/src/components/routes/system/charts/memory-charts.tsx` — wire `MemorySheet` into `MemoryChart`'s `cornerEl`
- `internal/site/src/locales/*/​*.po` (29 files, extraction only) + `internal/site/src/locales/es/es.po` (extraction + Spanish translations)

---

### Task 1: Agent — memory PSI parsing (pure function, TDD)

**Files:**
- Create: `agent/mem.go`
- Test: `agent/mem_test.go`

**Interfaces:**
- Produces: `func getMemPressure() (some [3]float64, full [3]float64)` — used by Task 2.
- Produces: `func parsePressureLine(fields string) [3]float64` — internal helper, used only within `agent/mem.go`.

- [ ] **Step 1: Write the failing test**

Create `agent/mem_test.go`:

```go
//go:build testing

package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePressureLine(t *testing.T) {
	t.Run("parses avg10, avg60, avg300", func(t *testing.T) {
		got := parsePressureLine("avg10=1.23 avg60=4.56 avg300=7.89 total=123456")
		assert.Equal(t, [3]float64{1.23, 4.56, 7.89}, got)
	})

	t.Run("ignores unknown fields", func(t *testing.T) {
		got := parsePressureLine("avg10=0.50 avg60=0.00 avg300=0.00 unknown=1 total=1")
		assert.Equal(t, [3]float64{0.50, 0, 0}, got)
	})

	t.Run("returns zero values for empty input", func(t *testing.T) {
		got := parsePressureLine("")
		assert.Equal(t, [3]float64{}, got)
	})

	t.Run("ignores malformed key=value pairs", func(t *testing.T) {
		got := parsePressureLine("avg10=1.00 malformed avg60=2.00")
		assert.Equal(t, [3]float64{1.00, 2.00, 0}, got)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go test -tags=testing ./agent/... -run TestParsePressureLine -v`
Expected: FAIL — `undefined: parsePressureLine` (compile error, since `agent/mem.go` doesn't exist yet).

- [ ] **Step 3: Write the implementation**

Create `agent/mem.go`:

```go
package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// getMemPressure reads Linux PSI (Pressure Stall Information) for memory from
// /proc/pressure/memory and returns the "some" and "full" stall percentages
// [avg10, avg60, avg300] each. Returns zero arrays on non-Linux systems or if
// the file is unavailable.
func getMemPressure() (some [3]float64, full [3]float64) {
	if runtime.GOOS != "linux" {
		return
	}
	f, err := os.Open("/proc/pressure/memory")
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

- [ ] **Step 4: Run the test to verify it passes**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go test -tags=testing ./agent/... -run TestParsePressureLine -v`
Expected: PASS, all 4 subtests green.

- [ ] **Step 5: Commit**

```bash
git add agent/mem.go agent/mem_test.go
git commit -m "$(cat <<'EOF'
feat(agent): add memory PSI (some+full) parsing

Reads /proc/pressure/memory on Linux, mirroring the existing CPU
pressure collection in agent/cpu.go but capturing both the "some"
and "full" stall lines (unlike CPU, which only has "some").

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Entities — add MemPressureSome/MemPressureFull to Stats struct

**Files:**
- Modify: `internal/entities/system/system.go:53` (insert after the `CpuPressure` field)

**Interfaces:**
- Consumes: nothing new.
- Produces: `system.Stats.MemPressureSome [3]float64` (json `memps`, cbor key 37), `system.Stats.MemPressureFull [3]float64` (json `mempf`, cbor key 38) — used by Task 3, 4, 6, 9.

- [ ] **Step 1: Add the fields**

In `internal/entities/system/system.go`, the `Stats` struct currently ends:

```go
	CpuPressure       [3]float64           `json:"cpup,omitempty" cbor:"36,keyasint,omitzero"`  // PSI some: [avg10, avg60, avg300]
}
```

Change to:

```go
	CpuPressure       [3]float64           `json:"cpup,omitempty" cbor:"36,keyasint,omitzero"`  // PSI some: [avg10, avg60, avg300]
	MemPressureSome   [3]float64           `json:"memps,omitempty" cbor:"37,keyasint,omitzero"` // memory PSI some: [avg10, avg60, avg300]
	MemPressureFull   [3]float64           `json:"mempf,omitempty" cbor:"38,keyasint,omitzero"` // memory PSI full: [avg10, avg60, avg300]
}
```

- [ ] **Step 2: Verify it compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go build ./internal/entities/...`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/entities/system/system.go
git commit -m "$(cat <<'EOF'
feat(entities): add MemPressureSome/MemPressureFull fields to Stats

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Agent — wire getMemPressure() into stats collection

**Files:**
- Modify: `agent/system.go:160-161`

**Interfaces:**
- Consumes: `getMemPressure() (some, full [3]float64)` from Task 1; `systemStats.MemPressureSome`/`MemPressureFull` fields from Task 2.

- [ ] **Step 1: Wire the call**

In `agent/system.go`, find:

```go
	// cpu pressure (PSI) - Linux only
	systemStats.CpuPressure = getCpuPressure()
```

Change to:

```go
	// cpu pressure (PSI) - Linux only
	systemStats.CpuPressure = getCpuPressure()

	// memory pressure (PSI) - Linux only
	systemStats.MemPressureSome, systemStats.MemPressureFull = getMemPressure()
```

- [ ] **Step 2: Verify it compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go build ./agent/...`
Expected: no output (success).

- [ ] **Step 3: Run the full agent test suite and confirm no new failures**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go test -tags=testing ./agent/... 2>&1 | grep -E "^--- FAIL|^ok|^FAIL"`
Expected: same 7 pre-existing `TestCollectorStartHelpers`/`TestNewGPUManagerPriority*` failures as the documented baseline (see Global Constraints) — no new failures. `TestParsePressureLine` from Task 1 passes.

- [ ] **Step 4: Commit**

```bash
git add agent/system.go
git commit -m "$(cat <<'EOF'
feat(agent): collect memory pressure PSI into system stats

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Alerts — add MemPressureSome/MemPressureFull to SystemAlertStats

**Files:**
- Modify: `internal/alerts/alerts.go:49-60`

**Interfaces:**
- Produces: `alerts.SystemAlertStats.MemPressureSome [3]float64` (json `memps`), `alerts.SystemAlertStats.MemPressureFull [3]float64` (json `mempf`) — used by Task 5.

- [ ] **Step 1: Add the fields**

In `internal/alerts/alerts.go`, find:

```go
// Values pulled from system_stats.stats that are relevant to alerts.
type SystemAlertStats struct {
	Cpu          float64                       `json:"cpu"`
	Mem          float64                       `json:"mp"`
	Disk         float64                       `json:"dp"`
	Bandwidth    [2]uint64                     `json:"b"`
	GPU          map[string]SystemAlertGPUData `json:"g"`
	Temperatures map[string]float32            `json:"t"`
	LoadAvg      [3]float64                    `json:"la"`
	Battery      [2]uint8                      `json:"bat"`
	ExtraFs      map[string]SystemAlertFsStats `json:"efs"`
	CpuPressure  [3]float64                    `json:"cpup"`
}
```

Change to:

```go
// Values pulled from system_stats.stats that are relevant to alerts.
type SystemAlertStats struct {
	Cpu             float64                       `json:"cpu"`
	Mem             float64                       `json:"mp"`
	Disk            float64                       `json:"dp"`
	Bandwidth       [2]uint64                     `json:"b"`
	GPU             map[string]SystemAlertGPUData `json:"g"`
	Temperatures    map[string]float32            `json:"t"`
	LoadAvg         [3]float64                    `json:"la"`
	Battery         [2]uint8                      `json:"bat"`
	ExtraFs         map[string]SystemAlertFsStats `json:"efs"`
	CpuPressure     [3]float64                    `json:"cpup"`
	MemPressureSome [3]float64                    `json:"memps"`
	MemPressureFull [3]float64                    `json:"mempf"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && mkdir -p internal/site/dist && touch internal/site/dist/index.html && go build ./internal/alerts/...`
Expected: no output (success). (The `mkdir`/`touch` step is the pre-existing embed placeholder from Global Constraints — only needed once per shell session/checkout.)

- [ ] **Step 3: Commit**

```bash
git add internal/alerts/alerts.go
git commit -m "$(cat <<'EOF'
feat(alerts): add MemPressureSome/MemPressureFull to SystemAlertStats

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Alerts — wire 6 memory pressure alert types into evaluation + naming

**Files:**
- Modify: `internal/alerts/alerts_system.go` (three locations: `HandleSystemAlerts` switch ~line 70-88, aggregation loop switch ~line 257-265, `sendSystemAlert` naming ~line 332-345)

**Interfaces:**
- Consumes: `data.Stats.MemPressureSome`/`MemPressureFull` (`SystemAlertStats`, Task 4).
- Produces: alert names `MemPressureSomeAvg10`, `MemPressureSomeAvg60`, `MemPressureSomeAvg300`, `MemPressureFullAvg10`, `MemPressureFullAvg60`, `MemPressureFullAvg300` recognized end-to-end — consumed by Task 7 (frontend `alertInfo` must use these exact strings as keys).

- [ ] **Step 1: Add cases to `HandleSystemAlerts`' per-check switch**

Find (in `internal/alerts/alerts_system.go`):

```go
		case "CpuPressureAvg300":
			if data.Stats.CpuPressure[2] == 0 {
				continue
			}
			val = data.Stats.CpuPressure[2]
			unit = "%"
		}
```

Change to:

```go
		case "CpuPressureAvg300":
			if data.Stats.CpuPressure[2] == 0 {
				continue
			}
			val = data.Stats.CpuPressure[2]
			unit = "%"
		case "MemPressureSomeAvg10":
			if data.Stats.MemPressureSome[0] == 0 {
				continue
			}
			val = data.Stats.MemPressureSome[0]
			unit = "%"
		case "MemPressureSomeAvg60":
			if data.Stats.MemPressureSome[1] == 0 {
				continue
			}
			val = data.Stats.MemPressureSome[1]
			unit = "%"
		case "MemPressureSomeAvg300":
			if data.Stats.MemPressureSome[2] == 0 {
				continue
			}
			val = data.Stats.MemPressureSome[2]
			unit = "%"
		case "MemPressureFullAvg10":
			if data.Stats.MemPressureFull[0] == 0 {
				continue
			}
			val = data.Stats.MemPressureFull[0]
			unit = "%"
		case "MemPressureFullAvg60":
			if data.Stats.MemPressureFull[1] == 0 {
				continue
			}
			val = data.Stats.MemPressureFull[1]
			unit = "%"
		case "MemPressureFullAvg300":
			if data.Stats.MemPressureFull[2] == 0 {
				continue
			}
			val = data.Stats.MemPressureFull[2]
			unit = "%"
		}
```

- [ ] **Step 2: Add cases to the aggregation loop's switch**

Find:

```go
			case "CpuPressureAvg10":
				alert.val += stats.CpuPressure[0]
			case "CpuPressureAvg60":
				alert.val += stats.CpuPressure[1]
			case "CpuPressureAvg300":
				alert.val += stats.CpuPressure[2]
			default:
				continue
			}
```

Change to:

```go
			case "CpuPressureAvg10":
				alert.val += stats.CpuPressure[0]
			case "CpuPressureAvg60":
				alert.val += stats.CpuPressure[1]
			case "CpuPressureAvg300":
				alert.val += stats.CpuPressure[2]
			case "MemPressureSomeAvg10":
				alert.val += stats.MemPressureSome[0]
			case "MemPressureSomeAvg60":
				alert.val += stats.MemPressureSome[1]
			case "MemPressureSomeAvg300":
				alert.val += stats.MemPressureSome[2]
			case "MemPressureFullAvg10":
				alert.val += stats.MemPressureFull[0]
			case "MemPressureFullAvg60":
				alert.val += stats.MemPressureFull[1]
			case "MemPressureFullAvg300":
				alert.val += stats.MemPressureFull[2]
			default:
				continue
			}
```

- [ ] **Step 3: Add name formatting + title-case exception in `sendSystemAlert`**

Find:

```go
	// format CpuPressure names
	if after, ok := strings.CutPrefix(alert.name, "CpuPressure"); ok {
		alert.name = "CPU Pressure " + after
	}

	// make title alert name lowercase if not CPU, GPU, or CPU Pressure
	titleAlertName := alert.name
	if titleAlertName != "CPU" && titleAlertName != "GPU" && !strings.HasPrefix(titleAlertName, "CPU Pressure") {
		titleAlertName = strings.ToLower(titleAlertName)
	}
```

Change to:

```go
	// format CpuPressure names
	if after, ok := strings.CutPrefix(alert.name, "CpuPressure"); ok {
		alert.name = "CPU Pressure " + after
	}
	// format MemPressure names
	if after, ok := strings.CutPrefix(alert.name, "MemPressureSome"); ok {
		alert.name = "Memory Pressure Some " + after
	} else if after, ok := strings.CutPrefix(alert.name, "MemPressureFull"); ok {
		alert.name = "Memory Pressure Full " + after
	}

	// make title alert name lowercase if not CPU, GPU, CPU Pressure, or Memory Pressure
	titleAlertName := alert.name
	if titleAlertName != "CPU" && titleAlertName != "GPU" && !strings.HasPrefix(titleAlertName, "CPU Pressure") &&
		!strings.HasPrefix(titleAlertName, "Memory Pressure") {
		titleAlertName = strings.ToLower(titleAlertName)
	}
```

- [ ] **Step 4: Verify it compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go build ./internal/alerts/...`
Expected: no output (success).

- [ ] **Step 5: Run targeted existing alert tests to confirm no regression**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go test -tags=testing ./internal/alerts/... -run 'TestSystemAlertsOneMin|TestSystemAlertsTwoMin' -v`
Expected: PASS (per Global Constraints, avoid running the full `./internal/alerts/...` package — it has a pre-existing unrelated panic).

- [ ] **Step 6: Commit**

```bash
git add internal/alerts/alerts_system.go
git commit -m "$(cat <<'EOF'
feat(alerts): wire 6 memory pressure alert types (some/full × avg10/60/300)

Same evaluation and predefined-level pattern as CPU pressure alerts.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Frontend types — add memps/mempf to SystemStats

**Files:**
- Modify: `internal/site/src/types.d.ts:92-93`

**Interfaces:**
- Produces: `SystemStats.memps?: [number, number, number]`, `SystemStats.mempf?: [number, number, number]` — used by Task 9 (`memory-pressure-chart.tsx`).

- [ ] **Step 1: Add the fields**

In `internal/site/src/types.d.ts`, find:

```ts
	/** cpu pressure PSI some [avg10, avg60, avg300] (%) */
	cpup?: [number, number, number]
```

Change to:

```ts
	/** cpu pressure PSI some [avg10, avg60, avg300] (%) */
	cpup?: [number, number, number]
	/** memory pressure PSI some [avg10, avg60, avg300] (%) */
	memps?: [number, number, number]
	/** memory pressure PSI full [avg10, avg60, avg300] (%) */
	mempf?: [number, number, number]
```

- [ ] **Step 2: Commit**

```bash
git add internal/site/src/types.d.ts
git commit -m "$(cat <<'EOF'
feat(types): add memps/mempf fields to SystemStats

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

(Full frontend typecheck happens in Task 12 once all frontend files exist and dependencies are installed.)

---

### Task 7: Frontend — add 6 alertInfo entries for memory pressure

**Files:**
- Modify: `internal/site/src/lib/alerts.ts:117-127` (insert after `CpuPressureAvg300`)

**Interfaces:**
- Consumes: `AlertInfo`/`AlertLevel` types (`internal/site/src/types.d.ts:326-345`, unchanged), `GaugeIcon` (already imported at the top of `alerts.ts`).
- Produces: `alertInfo.MemPressureSomeAvg10/60/300`, `alertInfo.MemPressureFullAvg10/60/300` — must use the exact same key strings as the backend switch cases added in Task 5.

- [ ] **Step 1: Add the entries**

In `internal/site/src/lib/alerts.ts`, find:

```ts
	CpuPressureAvg300: {
		name: () => t`CPU Pressure avg300`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 300s CPU pressure stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
} as const
```

Change to:

```ts
	CpuPressureAvg300: {
		name: () => t`CPU Pressure avg300`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 300s CPU pressure stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	MemPressureSomeAvg10: {
		name: () => t`Memory Pressure Some avg10`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 10s memory pressure some stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	MemPressureSomeAvg60: {
		name: () => t`Memory Pressure Some avg60`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 60s memory pressure some stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	MemPressureSomeAvg300: {
		name: () => t`Memory Pressure Some avg300`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 300s memory pressure some stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	MemPressureFullAvg10: {
		name: () => t`Memory Pressure Full avg10`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 10s memory pressure full stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	MemPressureFullAvg60: {
		name: () => t`Memory Pressure Full avg60`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 60s memory pressure full stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	MemPressureFullAvg300: {
		name: () => t`Memory Pressure Full avg300`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 300s memory pressure full stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
} as const
```

- [ ] **Step 2: Commit**

```bash
git add internal/site/src/lib/alerts.ts
git commit -m "$(cat <<'EOF'
feat(alerts-ui): add 6 memory pressure alert type definitions

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

(Full frontend typecheck happens in Task 12.)

---

### Task 8: Frontend — extract shared pressure badge/color/label helpers

**Files:**
- Create: `internal/site/src/components/routes/system/charts/pressure-utils.tsx`
- Modify: `internal/site/src/components/routes/system/charts/cpu-pressure-chart.tsx`

**Interfaces:**
- Produces: `pressureColor(value: number): string`, `pressureLabel(value: number): string`, `PressureBadge({ label, value }: { label: string; value: number })` — used by both `cpu-pressure-chart.tsx` (this task) and `memory-pressure-chart.tsx` (Task 9).

- [ ] **Step 1: Create the shared module**

Create `internal/site/src/components/routes/system/charts/pressure-utils.tsx`:

```tsx
import { t } from "@lingui/core/macro"
import { toFixedFloat } from "@/lib/utils"

export function pressureColor(value: number): string {
	if (value < 1) return "hsl(142, 71%, 45%)"
	if (value < 2) return "hsl(142, 60%, 55%)"
	if (value < 5) return "hsl(48, 96%, 45%)"
	if (value < 10) return "hsl(25, 95%, 53%)"
	return "hsl(0, 72%, 51%)"
}

export function pressureLabel(value: number): string {
	if (value < 1) return t`Excellent`
	if (value < 2) return t`Normal`
	if (value < 5) return t`Warning`
	if (value < 10) return t`High`
	return t`Critical`
}

export function PressureBadge({ label, value }: { label: string; value: number }) {
	const color = pressureColor(value)
	return (
		<div className="flex flex-col items-center gap-0.5 flex-1 py-1">
			<span className="text-xs text-muted-foreground font-medium tracking-wide">{label}</span>
			<span className="text-xl font-bold tabular-nums" style={{ color }}>
				{toFixedFloat(value, 2)}%
			</span>
			<span className="text-[11px] font-semibold" style={{ color }}>
				{pressureLabel(value)}
			</span>
		</div>
	)
}
```

- [ ] **Step 2: Update cpu-pressure-chart.tsx to use the shared module**

Replace the full contents of `internal/site/src/components/routes/system/charts/cpu-pressure-chart.tsx` with:

```tsx
import { t } from "@lingui/core/macro"
import LineChartDefault from "@/components/charts/line-chart"
import { toFixedFloat, cn } from "@/lib/utils"
import type { ChartData, SystemStatsRecord } from "@/types"
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import Spinner from "@/components/spinner"
import { useIntersectionObserver } from "@/lib/use-intersection-observer"
import { PressureBadge } from "./pressure-utils"

export function CpuPressureChart({
	chartData,
	grid,
	dataEmpty,
}: {
	chartData: ChartData
	grid: boolean
	dataEmpty: boolean
}) {
	const { isIntersecting, ref } = useIntersectionObserver()
	const latest = chartData.systemStats.at(-1)?.stats
	const pressure = latest?.cpup

	if (!pressure) {
		return null
	}

	const [avg10, avg60, avg300] = pressure

	return (
		<Card
			className={cn("px-3 py-5 sm:py-6 sm:px-6 min-h-auto", { "col-span-full": !grid })}
			ref={ref}
		>
			<CardHeader className="gap-1.5 p-0 mb-3 sm:mb-4">
				<CardTitle>{t`CPU Pressure (PSI)`}</CardTitle>
				<CardDescription>{t`% of time tasks stalled waiting for CPU — some stall`}</CardDescription>
			</CardHeader>

			{/* Current value badges */}
			<div className="flex justify-around mb-3 border border-border rounded-md py-2">
				<PressureBadge label="avg10" value={avg10} />
				<div className="w-px bg-border self-stretch" />
				<PressureBadge label="avg60" value={avg60} />
				<div className="w-px bg-border self-stretch" />
				<PressureBadge label="avg300" value={avg300} />
			</div>

			{/* Historical trend */}
			<div className="ps-0 -me-1 -ms-3.5 relative group h-54 md:h-56">
				<Spinner
					msg={dataEmpty ? t`Waiting for enough records to display` : undefined}
					className="group-has-[.opacity-100]:invisible duration-100"
				/>
				{isIntersecting && (
					<LineChartDefault
						chartData={chartData}
						legend={true}
						tickFormatter={(val) => `${toFixedFloat(val, 2)}%`}
						contentFormatter={({ value }) => `${toFixedFloat(value, 2)}%`}
						dataPoints={[
							{
								label: "avg10",
								color: "hsl(271, 81%, 60%)",
								dataKey: ({ stats }: SystemStatsRecord) => stats?.cpup?.[0],
							},
							{
								label: "avg60",
								color: "hsl(217, 91%, 60%)",
								dataKey: ({ stats }: SystemStatsRecord) => stats?.cpup?.[1],
							},
							{
								label: "avg300",
								color: "hsl(25, 95%, 53%)",
								dataKey: ({ stats }: SystemStatsRecord) => stats?.cpup?.[2],
							},
						]}
					/>
				)}
			</div>
		</Card>
	)
}
```

This removes the local `pressureColor`/`pressureLabel`/`PressureBadge` definitions (now imported from `./pressure-utils`) — everything else is byte-for-byte identical to the original, so `CpuPressureChart`'s rendered output is unchanged.

- [ ] **Step 3: Commit**

```bash
git add internal/site/src/components/routes/system/charts/pressure-utils.tsx internal/site/src/components/routes/system/charts/cpu-pressure-chart.tsx
git commit -m "$(cat <<'EOF'
refactor(ui): extract shared pressure badge/color/label helpers

Pulls pressureColor/pressureLabel/PressureBadge out of
cpu-pressure-chart.tsx into pressure-utils.tsx so the upcoming
Memory Pressure chart can reuse them instead of duplicating.
No behavior change to the existing CPU Pressure chart.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

(Full frontend typecheck happens in Task 12.)

---

### Task 9: Frontend — Memory Pressure chart component (Some + Full cards)

**Files:**
- Create: `internal/site/src/components/routes/system/charts/memory-pressure-chart.tsx`

**Interfaces:**
- Consumes: `PressureBadge` from `./pressure-utils` (Task 8); `SystemStats.memps`/`mempf` (Task 6); `LineChartDefault` (`@/components/charts/line-chart`, unchanged).
- Produces: `MemoryPressureChart({ chartData, grid, dataEmpty }: { chartData: ChartData; grid: boolean; dataEmpty: boolean })` — a React component, default export is NOT used (named export, matching `CpuPressureChart`'s convention) — used by Task 10 (`memory-sheet.tsx`).

- [ ] **Step 1: Create the component**

Create `internal/site/src/components/routes/system/charts/memory-pressure-chart.tsx`:

```tsx
import { t } from "@lingui/core/macro"
import LineChartDefault from "@/components/charts/line-chart"
import { toFixedFloat, cn } from "@/lib/utils"
import type { ChartData, SystemStatsRecord } from "@/types"
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import Spinner from "@/components/spinner"
import { useIntersectionObserver } from "@/lib/use-intersection-observer"
import { PressureBadge } from "./pressure-utils"

function MemoryPressureCard({
	chartData,
	grid,
	dataEmpty,
	title,
	description,
	statsKey,
}: {
	chartData: ChartData
	grid: boolean
	dataEmpty: boolean
	title: string
	description: string
	statsKey: "memps" | "mempf"
}) {
	const { isIntersecting, ref } = useIntersectionObserver()
	const latest = chartData.systemStats.at(-1)?.stats
	const pressure = latest?.[statsKey]

	if (!pressure) {
		return null
	}

	const [avg10, avg60, avg300] = pressure

	return (
		<Card
			className={cn("px-3 py-5 sm:py-6 sm:px-6 min-h-auto", { "col-span-full": !grid })}
			ref={ref}
		>
			<CardHeader className="gap-1.5 p-0 mb-3 sm:mb-4">
				<CardTitle>{title}</CardTitle>
				<CardDescription>{description}</CardDescription>
			</CardHeader>

			<div className="flex justify-around mb-3 border border-border rounded-md py-2">
				<PressureBadge label="avg10" value={avg10} />
				<div className="w-px bg-border self-stretch" />
				<PressureBadge label="avg60" value={avg60} />
				<div className="w-px bg-border self-stretch" />
				<PressureBadge label="avg300" value={avg300} />
			</div>

			<div className="ps-0 -me-1 -ms-3.5 relative group h-54 md:h-56">
				<Spinner
					msg={dataEmpty ? t`Waiting for enough records to display` : undefined}
					className="group-has-[.opacity-100]:invisible duration-100"
				/>
				{isIntersecting && (
					<LineChartDefault
						chartData={chartData}
						legend={true}
						tickFormatter={(val) => `${toFixedFloat(val, 2)}%`}
						contentFormatter={({ value }) => `${toFixedFloat(value, 2)}%`}
						dataPoints={[
							{
								label: "avg10",
								color: "hsl(271, 81%, 60%)",
								dataKey: ({ stats }: SystemStatsRecord) => stats?.[statsKey]?.[0],
							},
							{
								label: "avg60",
								color: "hsl(217, 91%, 60%)",
								dataKey: ({ stats }: SystemStatsRecord) => stats?.[statsKey]?.[1],
							},
							{
								label: "avg300",
								color: "hsl(25, 95%, 53%)",
								dataKey: ({ stats }: SystemStatsRecord) => stats?.[statsKey]?.[2],
							},
						]}
					/>
				)}
			</div>
		</Card>
	)
}

export function MemoryPressureChart({
	chartData,
	grid,
	dataEmpty,
}: {
	chartData: ChartData
	grid: boolean
	dataEmpty: boolean
}) {
	const latest = chartData.systemStats.at(-1)?.stats
	if (!latest?.memps && !latest?.mempf) {
		return null
	}

	return (
		<>
			<MemoryPressureCard
				chartData={chartData}
				grid={grid}
				dataEmpty={dataEmpty}
				title={t`Memory Pressure (PSI) — Some`}
				description={t`% of time tasks stalled waiting for memory — some stall`}
				statsKey="memps"
			/>
			<MemoryPressureCard
				chartData={chartData}
				grid={grid}
				dataEmpty={dataEmpty}
				title={t`Memory Pressure (PSI) — Full`}
				description={t`% of time tasks stalled waiting for memory — full stall`}
				statsKey="mempf"
			/>
		</>
	)
}
```

Notes on behavior: `MemoryPressureChart` returns `null` only if BOTH `memps` and `mempf` are absent from the latest stats snapshot. Each `MemoryPressureCard` independently guards its own data (`if (!pressure) return null`), so if an agent only reports one of the two (e.g. a future partial-support scenario), only the card with data renders.

- [ ] **Step 2: Commit**

```bash
git add internal/site/src/components/routes/system/charts/memory-pressure-chart.tsx
git commit -m "$(cat <<'EOF'
feat(ui): add Memory Pressure (PSI) chart with Some/Full cards

Mirrors CpuPressureChart's structure (badges + historical line chart)
but renders two stacked cards since memory PSI reports both "some"
and "full" stall, unlike CPU which only has "some".

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

(Full frontend typecheck happens in Task 12.)

---

### Task 10: Frontend — Memory "view more" sheet

**Files:**
- Create: `internal/site/src/components/routes/system/memory-sheet.tsx`

**Interfaces:**
- Consumes: `MemoryPressureChart` (Task 9, imported as `./charts/memory-pressure-chart`).
- Produces: default export `MemorySheet({ chartData, dataEmpty, grid }: { chartData: ChartData; dataEmpty: boolean; grid: boolean })` — used by Task 11 (`memory-charts.tsx`).

- [ ] **Step 1: Create the component**

Create `internal/site/src/components/routes/system/memory-sheet.tsx`, modeled on `disk-io-sheet.tsx` (no agent-version gate, unlike `cpu-sheet.tsx`, since there's no per-core-style breakdown involved here):

```tsx
import { t } from "@lingui/core/macro"
import { MoreHorizontalIcon } from "lucide-react"
import { memo, useRef, useState } from "react"
import ChartTimeSelect from "@/components/charts/chart-time-select"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTrigger } from "@/components/ui/sheet"
import { DialogTitle } from "@/components/ui/dialog"
import type { ChartData } from "@/types"
import { MemoryPressureChart } from "./charts/memory-pressure-chart"

export default memo(function MemorySheet({
	chartData,
	dataEmpty,
	grid,
}: {
	chartData: ChartData
	dataEmpty: boolean
	grid: boolean
}) {
	const [open, setOpen] = useState(false)
	const hasOpened = useRef(false)

	if (open && !hasOpened.current) {
		hasOpened.current = true
	}

	return (
		<Sheet open={open} onOpenChange={setOpen}>
			<DialogTitle className="sr-only">{t`Memory Usage`}</DialogTitle>
			<SheetTrigger asChild>
				<Button
					title={t`View more`}
					variant="outline"
					size="icon"
					className="shrink-0 max-sm:absolute max-sm:top-0 max-sm:end-0"
				>
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
	)
})
```

Both the `t\`Memory Usage\`` and `t\`View more\`` strings are already extracted (reused from `memory-charts.tsx`'s `ChartCard title` and the other `*-sheet.tsx` files respectively) — no new `msgid`s from this file.

- [ ] **Step 2: Commit**

```bash
git add internal/site/src/components/routes/system/memory-sheet.tsx
git commit -m "$(cat <<'EOF'
feat(ui): add MemorySheet "view more" panel for Memory Usage chart

Same Sheet/SheetTrigger pattern as disk-io-sheet.tsx — no agent
version gate (unlike cpu-sheet.tsx, which gates on the per-core
breakdown feature that doesn't apply here).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

(Full frontend typecheck happens in Task 12.)

---

### Task 11: Frontend — wire MemorySheet into the Memory Usage chart

**Files:**
- Modify: `internal/site/src/components/routes/system/charts/memory-charts.tsx`

**Interfaces:**
- Consumes: `MemorySheet` default export (Task 10, imported as `../memory-sheet`).

- [ ] **Step 1: Add the import**

In `internal/site/src/components/routes/system/charts/memory-charts.tsx`, find:

```tsx
import { t } from "@lingui/core/macro"
import AreaChartDefault from "@/components/charts/area-chart"
import { useContainerDataPoints } from "@/components/charts/hooks"
import { Unit } from "@/lib/enums"
import type { ChartConfig } from "@/components/ui/chart"
import type { ChartData, SystemStatsRecord } from "@/types"
import { ChartCard, FilterBar, SelectAvgMax } from "../chart-card"
import { dockerOrPodman } from "../chart-data"
import { decimalString, formatBytes, toFixedFloat } from "@/lib/utils"
import { pinnedAxisDomain } from "@/components/ui/chart"
```

Change to:

```tsx
import { t } from "@lingui/core/macro"
import AreaChartDefault from "@/components/charts/area-chart"
import { useContainerDataPoints } from "@/components/charts/hooks"
import { Unit } from "@/lib/enums"
import type { ChartConfig } from "@/components/ui/chart"
import type { ChartData, SystemStatsRecord } from "@/types"
import { ChartCard, FilterBar, SelectAvgMax } from "../chart-card"
import { dockerOrPodman } from "../chart-data"
import { decimalString, formatBytes, toFixedFloat } from "@/lib/utils"
import { pinnedAxisDomain } from "@/components/ui/chart"
import MemorySheet from "../memory-sheet"
```

- [ ] **Step 2: Wire the button into `cornerEl`**

In the same file, find (inside `MemoryChart`):

```tsx
	const maxValSelect = isLongerChart ? <SelectAvgMax max={maxValues} /> : null
	const totalMem = toFixedFloat(chartData.systemStats.at(-1)?.stats.m ?? 0, 1)

	return (
		<ChartCard
			empty={dataEmpty}
			grid={grid}
			title={t`Memory Usage`}
			description={t`Precise utilization at the recorded time`}
			cornerEl={maxValSelect}
		>
```

Change to:

```tsx
	const maxValSelect = isLongerChart ? <SelectAvgMax max={maxValues} /> : null
	const totalMem = toFixedFloat(chartData.systemStats.at(-1)?.stats.m ?? 0, 1)

	return (
		<ChartCard
			empty={dataEmpty}
			grid={grid}
			title={t`Memory Usage`}
			description={t`Precise utilization at the recorded time`}
			cornerEl={
				<div className="flex gap-2">
					{maxValSelect}
					<MemorySheet chartData={chartData} dataEmpty={dataEmpty} grid={grid} />
				</div>
			}
		>
```

This is the exact same wiring pattern as `CpuChart`'s `cornerEl` in `cpu-charts.tsx:35-40`. Only the `MemoryChart` function is touched — `ContainerMemoryChart` and `SwapChart` in the same file are untouched.

- [ ] **Step 3: Commit**

```bash
git add internal/site/src/components/routes/system/charts/memory-charts.tsx
git commit -m "$(cat <<'EOF'
feat(ui): add "view more" button to Memory Usage chart

Wires MemorySheet into MemoryChart's cornerEl, same pattern as
CpuCoresSheet on the CPU Usage chart.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 12: Frontend environment setup + typecheck/lint verification

**Files:** none (verification-only task)

**Interfaces:** none — this task verifies Tasks 6-11 all compile and lint together.

- [ ] **Step 1: Install frontend dependencies**

Run: `pnpm --dir internal/site install`
Expected: install completes. This will create `internal/site/node_modules` (gitignored) and, since this repo's canonical lockfile is `bun.lock` (not tracked by pnpm), pnpm will likely create `internal/site/pnpm-lock.yaml`. **Do not stage or commit this file** — it is a local-only substitute for `bun`, which isn't installed in this dev environment.

- [ ] **Step 2: Run the TypeScript build (typecheck)**

Run: `cd internal/site && pnpm exec tsc -b && cd ../..`
Expected: no output, exit code 0. If it fails, read the errors — they will point at exact file/line mismatches (e.g. a typo in a prop name between `memory-sheet.tsx` and `memory-pressure-chart.tsx`). Fix and re-run before proceeding.

- [ ] **Step 3: Run Biome lint/format check**

Run: `cd internal/site && pnpm exec biome check . && cd ../..`
Expected: no violations reported for the new/modified files (`pressure-utils.tsx`, `cpu-pressure-chart.tsx`, `memory-pressure-chart.tsx`, `memory-sheet.tsx`, `memory-charts.tsx`, `lib/alerts.ts`, `types.d.ts`). If Biome reports formatting issues, run `pnpm exec biome check --fix .` and review the diff before committing.

- [ ] **Step 4: Commit only if fixes were needed**

If Step 2 or Step 3 required code changes:

```bash
git add -u internal/site
git commit -m "$(cat <<'EOF'
fix(ui): resolve typecheck/lint issues in memory pressure panel

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

If no fixes were needed, skip this step (nothing to commit).

---

### Task 13: i18n — extract strings, translate Spanish, leave other 28 locales for Crowdin

**Files:**
- Modify (extraction, all 29 non-English locales get new empty `msgstr` entries): `internal/site/src/locales/*/*.po`
- Modify (extraction + Spanish translation): `internal/site/src/locales/es/es.po`
- Modify (extraction, source locale mirrors msgid as msgstr): `internal/site/src/locales/en/en.po`

**Interfaces:** none — this task only adds translation catalog entries for strings already introduced in Tasks 7 and 9 (`t\`...\`` calls in `lib/alerts.ts` and `memory-pressure-chart.tsx`).

- [ ] **Step 1: Run lingui extraction**

Run: `cd internal/site && pnpm exec lingui extract --overwrite && cd ../..`

Expected: this scans all `t\`...\`` usages (including the 10 new strings from Tasks 7 and 9 — see list below) and adds them as new `msgid` entries with empty `msgstr ""` to all 30 locale `.po` files (`en` plus the 29 configured locales), in alphabetically-sorted position. It does **not** remove any existing entries (no `--clean` flag, to keep the diff minimal and avoid touching unrelated translations).

The 10 new `msgid`s introduced by this feature are:
1. `Memory Pressure (PSI) — Some`
2. `Memory Pressure (PSI) — Full`
3. `% of time tasks stalled waiting for memory — some stall`
4. `% of time tasks stalled waiting for memory — full stall`
5. `Memory Pressure Some avg10`
6. `Memory Pressure Some avg60`
7. `Memory Pressure Some avg300`
8. `Memory Pressure Full avg10`
9. `Memory Pressure Full avg60`
10. `Memory Pressure Full avg300`

Plus 6 alert `desc` strings:
11. `Triggers when 10s memory pressure some stall exceeds a predefined level`
12. `Triggers when 60s memory pressure some stall exceeds a predefined level`
13. `Triggers when 300s memory pressure some stall exceeds a predefined level`
14. `Triggers when 10s memory pressure full stall exceeds a predefined level`
15. `Triggers when 60s memory pressure full stall exceeds a predefined level`
16. `Triggers when 300s memory pressure full stall exceeds a predefined level`

(The badge `avg10`/`avg60`/`avg300` labels and the `Warning (>2%)`/`High (>5%)`/`Critical (>10%)` level labels are plain strings or reused from the existing CPU Pressure entries — no new `msgid`s from those.)

- [ ] **Step 2: Verify extraction picked up all 16 new strings in es.po**

Run: `grep -c "Memory Pressure\|memory pressure some stall\|memory pressure full stall\|waiting for memory" internal/site/src/locales/es/es.po`
Expected: at least 16 (some strings may appear in multiple `#:` comment contexts if reused, but every one of the 16 `msgid`s listed above must be present).

- [ ] **Step 3: Fill in Spanish translations in es.po**

For each of the 16 new `msgid` entries in `internal/site/src/locales/es/es.po`, find the entry (extraction inserted it with `msgstr ""`) and set its `msgstr` using the Edit tool, one pair at a time. Use these exact translations:

| msgid | msgstr |
|---|---|
| `Memory Pressure (PSI) — Some` | `Presión de memoria (PSI) — Parcial` |
| `Memory Pressure (PSI) — Full` | `Presión de memoria (PSI) — Completa` |
| `% of time tasks stalled waiting for memory — some stall` | `% del tiempo que las tareas esperan memoria — stall parcial` |
| `% of time tasks stalled waiting for memory — full stall` | `% del tiempo que las tareas esperan memoria — stall completo` |
| `Memory Pressure Some avg10` | `Presión de memoria parcial avg10` |
| `Memory Pressure Some avg60` | `Presión de memoria parcial avg60` |
| `Memory Pressure Some avg300` | `Presión de memoria parcial avg300` |
| `Memory Pressure Full avg10` | `Presión de memoria completa avg10` |
| `Memory Pressure Full avg60` | `Presión de memoria completa avg60` |
| `Memory Pressure Full avg300` | `Presión de memoria completa avg300` |
| `Triggers when 10s memory pressure some stall exceeds a predefined level` | `Se activa cuando la presión de memoria parcial de 10s supera un nivel predefinido` |
| `Triggers when 60s memory pressure some stall exceeds a predefined level` | `Se activa cuando la presión de memoria parcial de 60s supera un nivel predefinido` |
| `Triggers when 300s memory pressure some stall exceeds a predefined level` | `Se activa cuando la presión de memoria parcial de 300s supera un nivel predefinido` |
| `Triggers when 10s memory pressure full stall exceeds a predefined level` | `Se activa cuando la presión de memoria completa de 10s supera un nivel predefinido` |
| `Triggers when 60s memory pressure full stall exceeds a predefined level` | `Se activa cuando la presión de memoria completa de 60s supera un nivel predefinido` |
| `Triggers when 300s memory pressure full stall exceeds a predefined level` | `Se activa cuando la presión de memoria completa de 300s supera un nivel predefinido` |

Example of the exact edit for the first row (same mechanical pattern for the other 15 — match on the `msgid` line and the immediately-following empty `msgstr ""` line):

```diff
 #: src/components/routes/system/charts/memory-pressure-chart.tsx
 msgid "Memory Pressure (PSI) — Some"
-msgstr ""
+msgstr "Presión de memoria (PSI) — Parcial"
```

- [ ] **Step 4: Compile catalogs**

Run: `cd internal/site && pnpm exec lingui compile && cd ../..`
Expected: no errors; this regenerates the compiled `.ts` catalog modules consumed at runtime (per `compileNamespace: "ts"` in `lingui.config.ts`) for all 30 locales, including the newly-filled Spanish strings.

- [ ] **Step 5: Confirm no unrelated locale files changed beyond new empty entries**

Run: `git diff --stat internal/site/src/locales | tail -5`
Expected: all 30 `locales/*/*.po` files show small additions (new `msgid`/`msgstr` pairs only), plus their compiled `.ts` counterparts. `es.po` is the only one with non-empty new `msgstr` values — spot check with:

Run: `git diff internal/site/src/locales/de/de.po | grep "^+msgstr" | grep -v '""'`
Expected: no output (German — or any locale besides `es`/`en` — should have no newly-populated non-empty `msgstr`).

- [ ] **Step 6: Commit**

```bash
git add internal/site/src/locales
git commit -m "$(cat <<'EOF'
i18n: extract Memory Pressure strings, add Spanish translations

Same pattern as the CPU Pressure i18n commit (a2082ddc): strings are
extracted into all 29 locale catalogs (empty msgstr, left for
Crowdin) and hand-translated only into Spanish.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 14: End-to-end manual verification in the browser

**Files:** none (verification-only task)

**Interfaces:** none.

- [ ] **Step 1: Build a placeholder dist and start the dev stack**

Since this feature needs live PSI data to visually confirm (real `/proc/pressure/memory` values require a Linux host — this dev machine is macOS, so `getMemPressure()` will always return zero arrays here, and the panel's cards will correctly render nothing):

Run: `export PATH="/opt/homebrew/bin:$PATH" && mkdir -p internal/site/dist && touch internal/site/dist/index.html`

Then start the frontend dev server: `cd internal/site && pnpm run dev --host 0.0.0.0`

In a second terminal, start the hub in dev mode: `export ENV=dev && export PATH="/opt/homebrew/bin:$PATH" && cd internal/cmd/hub && go run -tags development . serve --http 0.0.0.0:8090`

- [ ] **Step 2: Verify the button appears and the empty-data state is graceful**

Using the Chrome browser tool, navigate to the dev frontend, open a system's detail page, and confirm:
- The Memory Usage chart shows a "view more" icon button (matching the one already on CPU Usage) in its top-right corner.
- Clicking it opens a side panel with the time-range selector.
- Since this dev machine reports zero PSI values (macOS, not Linux), confirm the panel does **not** crash and does **not** show an empty/broken card — `MemoryPressureChart` should return `null` (no Memory Pressure cards rendered at all) because `getMemPressure()` returns zero arrays and `system.go`'s `omitzero` cbor tag means `memps`/`mempf` won't even be present in the payload for an all-zero result once persisted, OR (if the in-memory value differs) the cards should gracefully not render rather than showing a broken UI.
- Confirm the existing CPU Usage "view more" panel (CPU Pressure chart) still works exactly as before — the Task 8 refactor must not have changed its behavior.

- [ ] **Step 3: Verify the 6 new alert types in the alert configuration UI**

Navigate to the alerts configuration panel for a system (or the global alerts tab) and confirm all 6 new entries appear: "Memory Pressure Some avg10/60/300" and "Memory Pressure Full avg10/60/300", each showing a level `Select` dropdown (Warning/High/Critical) rather than a numeric slider — matching the existing CPU Pressure alert entries' UI.

- [ ] **Step 4: Report findings**

If anything in Steps 2-3 doesn't match expectations, fix the relevant task's files, re-run the affected verification commands, and commit the fix before proceeding. If everything matches, no further action needed — this is a verification-only task with no commit of its own.

---

## Self-Review Notes

- **Spec coverage:** §1 (agent collection) → Tasks 1, 3. §2 (data model) → Tasks 2, 4, 6. §3 (frontend components) → Tasks 8, 9, 10, 11. §4 (alerts) → Tasks 5, 7. §5 (i18n) → Task 13. §6 (verification) → Tasks 12, 14, plus targeted test runs embedded in Tasks 1, 3, 5.
- **Type consistency verified:** `statsKey: "memps" | "mempf"` (Task 9) matches the `SystemStats` field names added in Task 6. Alert key strings (`MemPressureSomeAvg10` etc.) are byte-identical between Task 5 (Go) and Task 7 (TypeScript). `MemPressureSome`/`MemPressureFull` field names are identical across Tasks 2, 4, 9.
- **No placeholders:** every task has complete, real code; the i18n task (13) has the full 16-row real-translation table instead of a "translate this" instruction.
