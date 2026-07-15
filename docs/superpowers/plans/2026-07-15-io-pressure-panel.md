# IO Pressure (PSI) Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add "IO Pressure (PSI)" (some + full, avg10/60/300) to the existing Disk I/O "view more" panel, plus 6 new alert types, mirroring the already-shipped Memory Pressure feature.

**Architecture:** New Linux-only PSI collection in the agent (`/proc/pressure/io`, both `some` and `full` lines) flows through the existing `Stats` blob (no new PocketBase collection) into a new section inside the *existing* `disk-io-sheet.tsx` "view more" panel (unlike CPU/Memory Pressure, Disk I/O already had a sheet before this feature). A shared `agent/psi.go` helper is extracted and reused by `cpu.go`/`mem.go`/`io.go` to eliminate a third copy of the same PSI-file-parsing loop. Six new alert types are wired the same way CPU/Memory Pressure alerts are. New UI strings are extracted for all 29 locales but only hand-translated into Spanish, matching this repo's real i18n workflow.

**Tech Stack:** Go 1.26 (agent, hub, entities, alerts), React 19 + TypeScript + Recharts + Lingui (frontend), PocketBase (storage, unchanged).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-15-io-pressure-panel-design.md` — every requirement in that spec must map to a task below.
- Backend `Stats` struct cbor keys: next free integers are `39` and `40` (highest used is `38`, `MemPressureFull`). Use exactly `39` for `IOPressureSome`, `40` for `IOPressureFull`.
- JSON field tags: `iodp` (some), `iodf` (full) — **NOT** `iops`/`iopf` (would be confusable with IOPS, an unrelated disk-throughput term). Used consistently across `internal/entities/system/system.go`, `internal/alerts/alerts.go`, and `internal/site/src/types.d.ts`.
- **Both new fields MUST use `omitzero` (not `omitempty`) on their JSON tags from the start.** Lesson learned from Memory Pressure: Go's `encoding/json` `omitempty` never omits a fixed-size array (`[N]T`) regardless of its values — its length is always `N`, never zero. Only `omitzero` (Go 1.24+) checks the actual zero value. Getting this wrong means the hub's JSON re-serialization of the CBOR-decoded `Stats` struct (see `internal/hub/systems/system.go` → PocketBase's `types.JSONRaw.Scan` → `json.Marshal`) would always include `"iodp":[0,0,0]`/`"iodf":[0,0,0]` for agents that never collected I/O PSI data (old agents, non-Linux hosts), defeating the frontend's presence check and showing a fake "Excellent 0.00%" panel instead of hiding it. The cbor tags should also use `omitzero` (matching the existing `CpuPressure`/`MemPressureSome`/`MemPressureFull` pattern).
- Alert type name strings (used as map keys end-to-end, frontend `alertInfo` and backend switch statements): `IOPressureSomeAvg10`, `IOPressureSomeAvg60`, `IOPressureSomeAvg300`, `IOPressureFullAvg10`, `IOPressureFullAvg60`, `IOPressureFullAvg300`. These exact strings must match byte-for-byte between `internal/site/src/lib/alerts.ts` and `internal/alerts/alerts_system.go`.
- The IO Pressure section renders **only when `!extraFsName`** in `disk-io-sheet.tsx` — `/proc/pressure/io` is a system-wide signal, not per-filesystem, so it must not appear (duplicated, misleadingly) under an extra-filesystem's panel.
- No PocketBase migration is needed anywhere in this plan — `stats` is a serialized blob field.
- **Environment note:** `go` is available via Homebrew (`export PATH="/opt/homebrew/bin:$PATH"` if a fresh shell doesn't have it on PATH).
- **Environment note:** `internal/site/embed.go` requires `internal/site/dist` to exist (via `//go:embed all:dist`) for any package that transitively imports `internal/site` (this includes `internal/alerts`) to build/test. If missing: `mkdir -p internal/site/dist && touch internal/site/dist/index.html` (matches the project's own `Makefile` `dev-hub`/`build-hub-dev` targets).
- **Environment note:** `go test -tags=testing ./agent/...` has pre-existing failures unrelated to this work (`TestCollectorStartHelpers`/`TestNewGPUManagerPriority*`, GPU collector tests needing host binaries not present in this dev environment). Confirm new work doesn't add failures beyond that known baseline — don't try to fix them.
- **Environment note:** `go test -tags=testing ./internal/alerts/...` panics intermittently when running the full package (pre-existing, unrelated to any of this work). Prefer running specific test names with `-run` to verify targeted behavior instead of the full package run.
- Frontend package manager: this repo's canonical lockfile is `internal/site/bun.lock` (tracked in git). `bun` is not installed in this dev environment; `pnpm` is available as a substitute for local verification (`pnpm --dir internal/site install`). **Never stage or commit `internal/site/pnpm-lock.yaml`, `internal/site/pnpm-workspace.yaml`, or `internal/site/node_modules`.** After frontend verification is done, delete `internal/site/node_modules` (and the two pnpm-only files if present) before any Docker rebuild — a stale local `node_modules` with pnpm's symlink layout has previously broken `docker compose build`'s `COPY internal/site/ ./` step (bun-based image expects its own layout).
- Frontend has no unit test runner configured (no `test` script in `package.json`). Verification for frontend tasks is: TypeScript build (`tsc -b`, `noEmit: true`), Biome lint/check, and manual browser/deployment verification — not unit tests.
- i18n scope: only `internal/site/src/locales/es/es.po` gets hand-written translations. The other 28 locales get the new `msgid`s added with empty `msgstr` via `lingui extract` (left for Crowdin), matching exactly what was done for CPU/Memory Pressure.
- This repo's test convention requires the `-tags=testing` flag on `go test` (e.g. `go test -tags=testing ./agent/...`).

---

## File Structure

**New files:**
- `agent/psi.go` — shared `readPressureFile()` + `parsePressureLine()` (moved from `agent/mem.go`)
- `agent/psi_test.go` — `parsePressureLine` unit tests (moved/renamed from `agent/mem_test.go`)
- `agent/io.go` — `getIOPressure()` (Linux PSI collection for I/O)
- `internal/site/src/components/routes/system/charts/io-pressure-chart.tsx` — `IOPressureChart` (Some + Full cards)

**Modified files:**
- `agent/mem.go` — shrinks to a one-line delegate to `readPressureFile`
- `agent/cpu.go` — `getCpuPressure()` shrinks to delegate to `readPressureFile`; unused imports (`bufio`, `os`, `strconv`, `strings`) removed
- `agent/system.go` — wire `getIOPressure()` into stats collection
- `internal/entities/system/system.go` — add `IOPressureSome`/`IOPressureFull` to `Stats`
- `internal/alerts/alerts.go` — add same fields to `SystemAlertStats`
- `internal/alerts/alerts_system.go` — add 6 alert switch cases (×2 places) + name formatting
- `internal/site/src/types.d.ts` — add `iodp`/`iodf` to `SystemStats`
- `internal/site/src/lib/alerts.ts` — add 6 `alertInfo` entries
- `internal/site/src/components/routes/system/disk-io-sheet.tsx` — wire `IOPressureChart` in (root filesystem only)
- `internal/site/src/locales/*/*.po` (29 files, extraction only) + `internal/site/src/locales/es/es.po` (extraction + Spanish translations)

---

### Task 1: Agent — extract shared psi.go helper, refactor mem.go to use it

**Files:**
- Create: `agent/psi.go`
- Create: `agent/psi_test.go` (moved from `agent/mem_test.go`)
- Modify: `agent/mem.go`
- Delete: `agent/mem_test.go` (superseded by `psi_test.go`)

**Interfaces:**
- Produces: `func readPressureFile(path string) (some [3]float64, full [3]float64)` — used by Task 2 (`cpu.go`), Task 3 (`io.go`), and this task's own `mem.go`.
- Produces: `func parsePressureLine(fields string) [3]float64` — internal helper, used only within `agent/psi.go`.
- Preserves: `func getMemPressure() (some [3]float64, full [3]float64)` — same signature as before, callers elsewhere are unaffected.

- [ ] **Step 1: Create the shared helper file**

Create `agent/psi.go`:

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

- [ ] **Step 2: Move the test file**

Create `agent/psi_test.go` with exactly the current contents of `agent/mem_test.go`:

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

Then delete `agent/mem_test.go` (its content is now in `psi_test.go` — leaving both would double-declare `TestParsePressureLine`).

- [ ] **Step 3: Shrink mem.go to delegate to the shared helper**

Replace the full contents of `agent/mem.go` with:

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

- [ ] **Step 4: Run the test to verify it still passes**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go test -tags=testing ./agent/... -run TestParsePressureLine -v`
Expected: PASS, all 4 subtests green (same as before the move — this confirms the refactor didn't change behavior).

- [ ] **Step 5: Verify the whole agent package still compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go build ./agent/...`
Expected: no output (success).

- [ ] **Step 6: Commit**

```bash
git add agent/psi.go agent/psi_test.go agent/mem.go
git rm agent/mem_test.go
git commit -m "$(cat <<'EOF'
refactor(agent): extract shared PSI file-parsing helper

Pulls readPressureFile/parsePressureLine out of mem.go into a new
psi.go so the upcoming IO Pressure collector (and a follow-up cpu.go
refactor) can reuse them instead of a third near-identical copy of
the same /proc/pressure/* scanning loop. getMemPressure's signature
and behavior are unchanged.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Agent — refactor cpu.go to use the shared psi.go helper

**Files:**
- Modify: `agent/cpu.go`

**Interfaces:**
- Consumes: `readPressureFile(path string) (some, full [3]float64)` from Task 1.
- Preserves: `func getCpuPressure() [3]float64` — same signature, callers elsewhere (`agent/system.go`) are unaffected.

- [ ] **Step 1: Shrink getCpuPressure to delegate**

In `agent/cpu.go`, find:

```go
// getCpuPressure reads Linux PSI (Pressure Stall Information) for CPU from
// /proc/pressure/cpu and returns the "some" stall percentages [avg10, avg60, avg300].
// Returns a zero array on non-Linux systems or if the file is unavailable.
func getCpuPressure() [3]float64 {
	if runtime.GOOS != "linux" {
		return [3]float64{}
	}
	f, err := os.Open("/proc/pressure/cpu")
	if err != nil {
		return [3]float64{}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		// Format: "some avg10=X.XX avg60=X.XX avg300=X.XX total=XXXXXX"
		var vals [3]float64
		for _, field := range strings.Fields(line[5:]) {
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
	return [3]float64{}
}
```

Replace with:

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

- [ ] **Step 2: Remove now-unused imports**

In `agent/cpu.go`, find:

```go
import (
	"bufio"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/shirou/gopsutil/v4/cpu"
)
```

Replace with:

```go
import (
	"math"
	"runtime"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/shirou/gopsutil/v4/cpu"
)
```

(`bufio`, `os`, `strconv`, `strings` were used only inside the old `getCpuPressure` body being replaced — verify this is true by checking the rest of the file still only references `math.` and `runtime.`, not `bufio.`/`os.`/`strconv.`/`strings.`, before removing.)

- [ ] **Step 3: Verify it compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go build ./agent/...`
Expected: no output (success). If it fails with "imported and not used" or "undefined", double-check Step 2's grep — some other function in `cpu.go` may still need one of those imports.

- [ ] **Step 4: Run the full agent test suite and confirm no new failures**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go test -tags=testing ./agent/... 2>&1 | grep -E "^--- FAIL|^ok|^FAIL"`
Expected: same pre-existing `TestCollectorStartHelpers`/`TestNewGPUManagerPriority*` failures as the documented baseline (see Global Constraints) — no new failures.

- [ ] **Step 5: Commit**

```bash
git add agent/cpu.go
git commit -m "$(cat <<'EOF'
refactor(agent): use shared readPressureFile helper in getCpuPressure

Same behavior, no signature change - cpu.go no longer needs its own
copy of the PSI file-scanning loop now that psi.go has it.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Agent — IO PSI collection (io.go)

**Files:**
- Create: `agent/io.go`

**Interfaces:**
- Consumes: `readPressureFile(path string) (some, full [3]float64)` from Task 1.
- Produces: `func getIOPressure() (some [3]float64, full [3]float64)` — used by Task 5 (`agent/system.go`).

- [ ] **Step 1: Create the file**

Create `agent/io.go`:

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

- [ ] **Step 2: Verify it compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go build ./agent/...`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add agent/io.go
git commit -m "$(cat <<'EOF'
feat(agent): add IO PSI (some+full) collection

Reads /proc/pressure/io on Linux via the shared readPressureFile
helper, mirroring memory pressure's some+full structure (unlike CPU,
which only has "some").

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Entities — add IOPressureSome/IOPressureFull to Stats struct

**Files:**
- Modify: `internal/entities/system/system.go` (insert after the `MemPressureFull` field)

**Interfaces:**
- Consumes: nothing new.
- Produces: `system.Stats.IOPressureSome [3]float64` (json `iodp`, cbor key 39), `system.Stats.IOPressureFull [3]float64` (json `iodf`, cbor key 40) — used by Task 5, 6, 10.

- [ ] **Step 1: Add the fields**

In `internal/entities/system/system.go`, the `Stats` struct currently ends:

```go
	MemPressureFull   [3]float64           `json:"mempf,omitzero" cbor:"38,keyasint,omitzero"`  // memory PSI full: [avg10, avg60, avg300]
}
```

Change to:

```go
	MemPressureFull   [3]float64           `json:"mempf,omitzero" cbor:"38,keyasint,omitzero"`  // memory PSI full: [avg10, avg60, avg300]
	IOPressureSome    [3]float64           `json:"iodp,omitzero" cbor:"39,keyasint,omitzero"`   // io PSI some: [avg10, avg60, avg300]
	IOPressureFull    [3]float64           `json:"iodf,omitzero" cbor:"40,keyasint,omitzero"`   // io PSI full: [avg10, avg60, avg300]
}
```

Then run `gofmt -w internal/entities/system/system.go` to fix struct-tag column alignment (the new, longer field names shift the alignment of the whole block).

- [ ] **Step 2: Verify it compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go build ./internal/entities/...`
Expected: no output (success).

- [ ] **Step 3: Add a serialization regression test**

This repo has an existing test guarding exactly this class of bug (`TestStatsPressureFieldsOmittedWhenZero`/`TestStatsPressureFieldsPresentWhenNonZero` in `internal/entities/system/system_test.go`, added after Memory Pressure's `omitzero` bug was found in final review). Extend both existing tests to also cover `iodp`/`iodf`:

In `internal/entities/system/system_test.go`, find:

```go
	for _, key := range []string{"memps", "mempf", "cpup"} {
		if _, present := raw[key]; present {
			t.Errorf("expected %q to be omitted for a zero-value Stats, got: %s", key, raw[key])
		}
	}
}
```

Change to:

```go
	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf"} {
		if _, present := raw[key]; present {
			t.Errorf("expected %q to be omitted for a zero-value Stats, got: %s", key, raw[key])
		}
	}
}
```

And find:

```go
func TestStatsPressureFieldsPresentWhenNonZero(t *testing.T) {
	s := Stats{
		MemPressureSome: [3]float64{1.1, 2.2, 3.3},
		MemPressureFull: [3]float64{4.4, 5.5, 6.6},
		CpuPressure:     [3]float64{7.7, 8.8, 9.9},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal populated Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup"} {
		if _, present := raw[key]; !present {
			t.Errorf("expected %q to be present for a non-zero Stats", key)
		}
	}
}
```

Change to:

```go
func TestStatsPressureFieldsPresentWhenNonZero(t *testing.T) {
	s := Stats{
		MemPressureSome: [3]float64{1.1, 2.2, 3.3},
		MemPressureFull: [3]float64{4.4, 5.5, 6.6},
		CpuPressure:     [3]float64{7.7, 8.8, 9.9},
		IOPressureSome:  [3]float64{1.2, 3.4, 5.6},
		IOPressureFull:  [3]float64{7.8, 9.0, 1.2},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal populated Stats: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"memps", "mempf", "cpup", "iodp", "iodf"} {
		if _, present := raw[key]; !present {
			t.Errorf("expected %q to be present for a non-zero Stats", key)
		}
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go test -tags=testing ./internal/entities/system/... -v`
Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/entities/system/system.go internal/entities/system/system_test.go
git commit -m "$(cat <<'EOF'
feat(entities): add IOPressureSome/IOPressureFull fields to Stats

Uses omitzero (not omitempty) on the JSON tags from the start - see
the Memory Pressure fix (5e6da737) for why omitempty never omits a
fixed-size array. Extends the existing serialization regression test
to cover iodp/iodf too.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Agent — wire getIOPressure() into stats collection

**Files:**
- Modify: `agent/system.go`

**Interfaces:**
- Consumes: `getIOPressure() (some, full [3]float64)` from Task 3; `systemStats.IOPressureSome`/`IOPressureFull` fields from Task 4.

- [ ] **Step 1: Wire the call**

In `agent/system.go`, find:

```go
	// memory pressure (PSI) - Linux only
	systemStats.MemPressureSome, systemStats.MemPressureFull = getMemPressure()
```

Change to:

```go
	// memory pressure (PSI) - Linux only
	systemStats.MemPressureSome, systemStats.MemPressureFull = getMemPressure()

	// io pressure (PSI) - Linux only
	systemStats.IOPressureSome, systemStats.IOPressureFull = getIOPressure()
```

- [ ] **Step 2: Verify it compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go build ./agent/...`
Expected: no output (success).

- [ ] **Step 3: Run the full agent test suite and confirm no new failures**

Run: `export PATH="/opt/homebrew/bin:$PATH" && go test -tags=testing ./agent/... 2>&1 | grep -E "^--- FAIL|^ok|^FAIL"`
Expected: same pre-existing GPU-collector failures as baseline, nothing new.

- [ ] **Step 4: Commit**

```bash
git add agent/system.go
git commit -m "$(cat <<'EOF'
feat(agent): collect IO pressure PSI into system stats

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Alerts — add IOPressureSome/IOPressureFull to SystemAlertStats

**Files:**
- Modify: `internal/alerts/alerts.go`

**Interfaces:**
- Produces: `alerts.SystemAlertStats.IOPressureSome [3]float64` (json `iodp`), `alerts.SystemAlertStats.IOPressureFull [3]float64` (json `iodf`) — used by Task 7.

- [ ] **Step 1: Add the fields**

In `internal/alerts/alerts.go`, find:

```go
	CpuPressure     [3]float64                    `json:"cpup"`
	MemPressureSome [3]float64                    `json:"memps"`
	MemPressureFull [3]float64                    `json:"mempf"`
}
```

Change to:

```go
	CpuPressure     [3]float64                    `json:"cpup"`
	MemPressureSome [3]float64                    `json:"memps"`
	MemPressureFull [3]float64                    `json:"mempf"`
	IOPressureSome  [3]float64                    `json:"iodp"`
	IOPressureFull  [3]float64                    `json:"iodf"`
}
```

Then run `gofmt -w internal/alerts/alerts.go`.

- [ ] **Step 2: Verify it compiles**

Run: `export PATH="/opt/homebrew/bin:$PATH" && mkdir -p internal/site/dist && touch internal/site/dist/index.html && go build ./internal/alerts/...`
Expected: no output (success). (The `mkdir`/`touch` step is the pre-existing embed placeholder from Global Constraints — only needed once per shell session/checkout.)

- [ ] **Step 3: Commit**

```bash
git add internal/alerts/alerts.go
git commit -m "$(cat <<'EOF'
feat(alerts): add IOPressureSome/IOPressureFull to SystemAlertStats

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Alerts — wire 6 IO pressure alert types into evaluation + naming

**Files:**
- Modify: `internal/alerts/alerts_system.go` (three locations: `HandleSystemAlerts` switch, aggregation loop switch, `sendSystemAlert` naming)

**Interfaces:**
- Consumes: `data.Stats.IOPressureSome`/`IOPressureFull` (`SystemAlertStats`, Task 6).
- Produces: alert names `IOPressureSomeAvg10`, `IOPressureSomeAvg60`, `IOPressureSomeAvg300`, `IOPressureFullAvg10`, `IOPressureFullAvg60`, `IOPressureFullAvg300` recognized end-to-end — consumed by Task 9 (frontend `alertInfo` must use these exact strings as keys).

- [ ] **Step 1: Add cases to `HandleSystemAlerts`' per-check switch**

Find (in `internal/alerts/alerts_system.go`):

```go
		case "MemPressureFullAvg300":
			if data.Stats.MemPressureFull[2] == 0 {
				continue
			}
			val = data.Stats.MemPressureFull[2]
			unit = "%"
		}
```

Change to:

```go
		case "MemPressureFullAvg300":
			if data.Stats.MemPressureFull[2] == 0 {
				continue
			}
			val = data.Stats.MemPressureFull[2]
			unit = "%"
		case "IOPressureSomeAvg10":
			if data.Stats.IOPressureSome[0] == 0 {
				continue
			}
			val = data.Stats.IOPressureSome[0]
			unit = "%"
		case "IOPressureSomeAvg60":
			if data.Stats.IOPressureSome[1] == 0 {
				continue
			}
			val = data.Stats.IOPressureSome[1]
			unit = "%"
		case "IOPressureSomeAvg300":
			if data.Stats.IOPressureSome[2] == 0 {
				continue
			}
			val = data.Stats.IOPressureSome[2]
			unit = "%"
		case "IOPressureFullAvg10":
			if data.Stats.IOPressureFull[0] == 0 {
				continue
			}
			val = data.Stats.IOPressureFull[0]
			unit = "%"
		case "IOPressureFullAvg60":
			if data.Stats.IOPressureFull[1] == 0 {
				continue
			}
			val = data.Stats.IOPressureFull[1]
			unit = "%"
		case "IOPressureFullAvg300":
			if data.Stats.IOPressureFull[2] == 0 {
				continue
			}
			val = data.Stats.IOPressureFull[2]
			unit = "%"
		}
```

- [ ] **Step 2: Add cases to the aggregation loop's switch**

Find:

```go
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

Change to:

```go
			case "MemPressureFullAvg10":
				alert.val += stats.MemPressureFull[0]
			case "MemPressureFullAvg60":
				alert.val += stats.MemPressureFull[1]
			case "MemPressureFullAvg300":
				alert.val += stats.MemPressureFull[2]
			case "IOPressureSomeAvg10":
				alert.val += stats.IOPressureSome[0]
			case "IOPressureSomeAvg60":
				alert.val += stats.IOPressureSome[1]
			case "IOPressureSomeAvg300":
				alert.val += stats.IOPressureSome[2]
			case "IOPressureFullAvg10":
				alert.val += stats.IOPressureFull[0]
			case "IOPressureFullAvg60":
				alert.val += stats.IOPressureFull[1]
			case "IOPressureFullAvg300":
				alert.val += stats.IOPressureFull[2]
			default:
				continue
			}
```

- [ ] **Step 3: Add name formatting + title-case exception in `sendSystemAlert`**

Find:

```go
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

Change to:

```go
	// format MemPressure names
	if after, ok := strings.CutPrefix(alert.name, "MemPressureSome"); ok {
		alert.name = "Memory Pressure Some " + after
	} else if after, ok := strings.CutPrefix(alert.name, "MemPressureFull"); ok {
		alert.name = "Memory Pressure Full " + after
	}
	// format IOPressure names
	if after, ok := strings.CutPrefix(alert.name, "IOPressureSome"); ok {
		alert.name = "IO Pressure Some " + after
	} else if after, ok := strings.CutPrefix(alert.name, "IOPressureFull"); ok {
		alert.name = "IO Pressure Full " + after
	}

	// make title alert name lowercase if not CPU, GPU, CPU Pressure, Memory Pressure, or IO Pressure
	titleAlertName := alert.name
	if titleAlertName != "CPU" && titleAlertName != "GPU" && !strings.HasPrefix(titleAlertName, "CPU Pressure") &&
		!strings.HasPrefix(titleAlertName, "Memory Pressure") && !strings.HasPrefix(titleAlertName, "IO Pressure") {
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
feat(alerts): wire 6 IO pressure alert types (some/full × avg10/60/300)

Same evaluation and predefined-level pattern as CPU/Memory pressure
alerts.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Frontend types — add iodp/iodf to SystemStats

**Files:**
- Modify: `internal/site/src/types.d.ts`

**Interfaces:**
- Produces: `SystemStats.iodp?: [number, number, number]`, `SystemStats.iodf?: [number, number, number]` — used by Task 10 (`io-pressure-chart.tsx`).

- [ ] **Step 1: Add the fields**

In `internal/site/src/types.d.ts`, find:

```ts
	/** memory pressure PSI some [avg10, avg60, avg300] (%) */
	memps?: [number, number, number]
	/** memory pressure PSI full [avg10, avg60, avg300] (%) */
	mempf?: [number, number, number]
```

Change to:

```ts
	/** memory pressure PSI some [avg10, avg60, avg300] (%) */
	memps?: [number, number, number]
	/** memory pressure PSI full [avg10, avg60, avg300] (%) */
	mempf?: [number, number, number]
	/** io pressure PSI some [avg10, avg60, avg300] (%) */
	iodp?: [number, number, number]
	/** io pressure PSI full [avg10, avg60, avg300] (%) */
	iodf?: [number, number, number]
```

- [ ] **Step 2: Commit**

```bash
git add internal/site/src/types.d.ts
git commit -m "$(cat <<'EOF'
feat(types): add iodp/iodf fields to SystemStats

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

(Full frontend typecheck happens in Task 12 once all frontend files exist and dependencies are installed.)

---

### Task 9: Frontend — add 6 alertInfo entries for IO pressure

**Files:**
- Modify: `internal/site/src/lib/alerts.ts` (insert after `MemPressureFullAvg300`, before `} as const`)

**Interfaces:**
- Consumes: `AlertInfo`/`AlertLevel` types (unchanged), `GaugeIcon` (already imported at the top of `alerts.ts`).
- Produces: `alertInfo.IOPressureSomeAvg10/60/300`, `alertInfo.IOPressureFullAvg10/60/300` — must use the exact same key strings as the backend switch cases added in Task 7.

- [ ] **Step 1: Add the entries**

In `internal/site/src/lib/alerts.ts`, find:

```ts
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

Change to:

```ts
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
	IOPressureSomeAvg10: {
		name: () => t`IO Pressure Some avg10`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 10s IO pressure some stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	IOPressureSomeAvg60: {
		name: () => t`IO Pressure Some avg60`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 60s IO pressure some stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	IOPressureSomeAvg300: {
		name: () => t`IO Pressure Some avg300`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 300s IO pressure some stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	IOPressureFullAvg10: {
		name: () => t`IO Pressure Full avg10`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 10s IO pressure full stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	IOPressureFullAvg60: {
		name: () => t`IO Pressure Full avg60`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 60s IO pressure full stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	IOPressureFullAvg300: {
		name: () => t`IO Pressure Full avg300`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 300s IO pressure full stall exceeds a predefined level`,
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
feat(alerts-ui): add 6 IO pressure alert type definitions

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

(Full frontend typecheck happens in Task 12.)

---

### Task 10: Frontend — IO Pressure chart component (Some + Full cards)

**Files:**
- Create: `internal/site/src/components/routes/system/charts/io-pressure-chart.tsx`

**Interfaces:**
- Consumes: `PressureBadge` from `./pressure-utils` (already exists, extracted during Memory Pressure — do not modify it); `SystemStats.iodp`/`iodf` (Task 8); `LineChartDefault` (`@/components/charts/line-chart`, unchanged).
- Produces: `IOPressureChart({ chartData, grid, dataEmpty }: { chartData: ChartData; grid: boolean; dataEmpty: boolean })` — a React named-export component — used by Task 11 (`disk-io-sheet.tsx`).

- [ ] **Step 1: Create the component**

Create `internal/site/src/components/routes/system/charts/io-pressure-chart.tsx`:

```tsx
import { t } from "@lingui/core/macro"
import LineChartDefault from "@/components/charts/line-chart"
import { toFixedFloat, cn } from "@/lib/utils"
import type { ChartData, SystemStatsRecord } from "@/types"
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import Spinner from "@/components/spinner"
import { useIntersectionObserver } from "@/lib/use-intersection-observer"
import { PressureBadge } from "./pressure-utils"

function IOPressureCard({
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
	statsKey: "iodp" | "iodf"
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

export function IOPressureChart({
	chartData,
	grid,
	dataEmpty,
}: {
	chartData: ChartData
	grid: boolean
	dataEmpty: boolean
}) {
	const latest = chartData.systemStats.at(-1)?.stats
	if (!latest?.iodp && !latest?.iodf) {
		return null
	}

	return (
		<>
			<IOPressureCard
				chartData={chartData}
				grid={grid}
				dataEmpty={dataEmpty}
				title={t`IO Pressure (PSI) — Some`}
				description={t`% of time tasks stalled waiting for I/O — some stall`}
				statsKey="iodp"
			/>
			<IOPressureCard
				chartData={chartData}
				grid={grid}
				dataEmpty={dataEmpty}
				title={t`IO Pressure (PSI) — Full`}
				description={t`% of time tasks stalled waiting for I/O — full stall`}
				statsKey="iodf"
			/>
		</>
	)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/site/src/components/routes/system/charts/io-pressure-chart.tsx
git commit -m "$(cat <<'EOF'
feat(ui): add IO Pressure (PSI) chart with Some/Full cards

Mirrors MemoryPressureChart's structure (badges + historical line
chart), reusing the shared pressure-utils.tsx helpers.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

(Full frontend typecheck happens in Task 12.)

---

### Task 11: Frontend — wire IOPressureChart into the existing Disk I/O sheet

**Files:**
- Modify: `internal/site/src/components/routes/system/disk-io-sheet.tsx`

**Interfaces:**
- Consumes: `IOPressureChart` (Task 10, imported as `./charts/io-pressure-chart`).

- [ ] **Step 1: Add the import**

In `internal/site/src/components/routes/system/disk-io-sheet.tsx`, find:

```tsx
import { diskDataFns, DiskUtilizationChart } from "./charts/disk-charts"
import { pinnedAxisDomain } from "@/components/ui/chart"
```

Change to:

```tsx
import { diskDataFns, DiskUtilizationChart } from "./charts/disk-charts"
import { pinnedAxisDomain } from "@/components/ui/chart"
import { IOPressureChart } from "./charts/io-pressure-chart"
```

- [ ] **Step 2: Insert the section (root filesystem only)**

In the same file, find the closing of the main throughput `ChartCard` and the following `DiskUtilizationChart` line:

```tsx
					</ChartCard>

					{hasUtilization && <DiskUtilizationChart systemData={systemData} extraFsName={extraFsName} />}
```

Change to:

```tsx
					</ChartCard>

					{!extraFsName && <IOPressureChart chartData={chartData} grid={grid} dataEmpty={dataEmpty} />}

					{hasUtilization && <DiskUtilizationChart systemData={systemData} extraFsName={extraFsName} />}
```

`chartData`, `grid`, and `dataEmpty` are already destructured from `systemData` at the top of `DiskIOSheet` (`const { chartData, grid, dataEmpty, showMax, maxValues, isLongerChart } = systemData`) — no new prop plumbing needed. The `!extraFsName` guard ensures this section only renders for the root filesystem's panel, not for extra-filesystem panels (per the design spec — `/proc/pressure/io` is system-wide, not per-filesystem).

- [ ] **Step 3: Commit**

```bash
git add internal/site/src/components/routes/system/disk-io-sheet.tsx
git commit -m "$(cat <<'EOF'
feat(ui): add IO Pressure section to the Disk I/O "view more" panel

Renders only for the root filesystem (not extra filesystems) since
/proc/pressure/io is a system-wide signal, not per-device. No other
part of disk-io-sheet.tsx changes - DiskUtilizationChart, I/O Time,
Queue Depth, and I/O Await sections are untouched.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 12: Frontend environment setup + typecheck/lint verification

**Files:** none (verification-only task)

**Interfaces:** none — this task verifies Tasks 8-11 all compile and lint together.

- [ ] **Step 1: Install frontend dependencies**

Run: `pnpm --dir internal/site install`
Expected: install completes, creating `internal/site/node_modules` (gitignored) and possibly `internal/site/pnpm-lock.yaml`/`internal/site/pnpm-workspace.yaml` (pnpm-specific, needed to make `pnpm exec` work in this dev environment — a `pnpm-workspace.yaml` with `allowBuilds`/`shamefullyHoist: true` may be required; see the Memory Pressure plan's Task 12 for the exact prior resolution if `pnpm install` fails on a build-script approval gate or phantom-dependency resolution errors). **Do not stage or commit any of these** — they are local-only substitutes for `bun`, which isn't installed in this dev environment.

- [ ] **Step 2: Run the TypeScript build (typecheck)**

Run: `cd internal/site && pnpm exec tsc -b && cd ../..`
Expected: no output, exit code 0. If it fails, read the errors — they will point at exact file/line mismatches. Fix and re-run before proceeding. Do NOT fix unrelated pre-existing errors outside this feature's files without asking the user first (this happened during Memory Pressure's equivalent task and required a follow-up decision — see commit `dc7daa46` and its fix commits `599d0717`/`177053da` for the precedent of what NOT to silently repeat: a similarly-motivated cleanup there broke `lingui extract` for the whole project by changing a `useLingui()` call into an unsupported bare-statement form).

- [ ] **Step 3: Run Biome lint/format check**

Run: `cd internal/site && pnpm exec biome check src/components/routes/system/charts/io-pressure-chart.tsx src/components/routes/system/disk-io-sheet.tsx src/lib/alerts.ts src/types.d.ts && cd ../..`
Expected: exit 0, 0 errors for these specific new/modified files (scoped check — the whole-repo `biome check .` has pre-existing, unrelated violations in ~25 other files, not this feature's concern). If Biome reports formatting issues in these files, run `pnpm exec biome check --fix` scoped to just them and review the diff before committing.

- [ ] **Step 4: Commit only if fixes were needed**

If Step 2 or Step 3 required code changes:

```bash
git add -u internal/site
git commit -m "$(cat <<'EOF'
fix(ui): resolve typecheck/lint issues in IO pressure panel

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

If no fixes were needed, skip this step (nothing to commit).

- [ ] **Step 5: Clean up local frontend tooling artifacts**

Run: `rm -rf internal/site/node_modules internal/site/pnpm-lock.yaml internal/site/pnpm-workspace.yaml`

This prevents a stale pnpm-installed `node_modules` (different symlink layout than `bun install` produces) from breaking a later `docker compose build` — this exact failure mode (`cannot replace to directory ... with file`) occurred during Memory Pressure's equivalent step and required deleting these same files before the Docker rebuild would succeed.

---

### Task 13: i18n — extract strings, translate Spanish, leave other 28 locales for Crowdin

**Files:**
- Modify (extraction, all 29 non-English locales get new empty `msgstr` entries): `internal/site/src/locales/*/*.po`
- Modify (extraction + Spanish translation): `internal/site/src/locales/es/es.po`
- Modify (extraction, source locale mirrors msgid as msgstr): `internal/site/src/locales/en/en.po`

**Interfaces:** none — this task only adds translation catalog entries for strings already introduced in Tasks 9 and 10 (`t\`...\`` calls in `lib/alerts.ts` and `io-pressure-chart.tsx`).

- [ ] **Step 1: Reinstall frontend dependencies** (Task 12 cleaned them up)

Run: `pnpm --dir internal/site install`

- [ ] **Step 2: Run lingui extraction**

Run: `cd internal/site && pnpm exec lingui extract --overwrite && cd ../..`

Expected: this scans all `t\`...\`` usages (including the 16 new strings from Tasks 9 and 10 — see list below) and adds them as new `msgid` entries with empty `msgstr ""` to all 30 locale `.po` files, in alphabetically-sorted position. No `--clean` flag (keeps the diff minimal, doesn't prune unrelated entries).

The 16 new `msgid`s introduced by this feature are:
1. `IO Pressure (PSI) — Some`
2. `IO Pressure (PSI) — Full`
3. `% of time tasks stalled waiting for I/O — some stall`
4. `% of time tasks stalled waiting for I/O — full stall`
5. `IO Pressure Some avg10`
6. `IO Pressure Some avg60`
7. `IO Pressure Some avg300`
8. `IO Pressure Full avg10`
9. `IO Pressure Full avg60`
10. `IO Pressure Full avg300`
11. `Triggers when 10s IO pressure some stall exceeds a predefined level`
12. `Triggers when 60s IO pressure some stall exceeds a predefined level`
13. `Triggers when 300s IO pressure some stall exceeds a predefined level`
14. `Triggers when 10s IO pressure full stall exceeds a predefined level`
15. `Triggers when 60s IO pressure full stall exceeds a predefined level`
16. `Triggers when 300s IO pressure full stall exceeds a predefined level`

(The badge `avg10`/`avg60`/`avg300` labels and the `Warning (>2%)`/`High (>5%)`/`Critical (>10%)` level labels are reused from the existing CPU/Memory Pressure entries — no new `msgid`s from those. `Waiting for enough records to display` is also already extracted.)

- [ ] **Step 3: Verify extraction picked up all 16 new strings in es.po**

Run: `grep -c "IO Pressure\|waiting for I/O" internal/site/src/locales/es/es.po`
Expected: at least 16.

- [ ] **Step 4: Fill in Spanish translations in es.po**

For each of the 16 new `msgid` entries in `internal/site/src/locales/es/es.po`, find the entry (extraction inserted it with `msgstr ""`) and set its `msgstr` using the Edit tool. Use these exact translations:

| msgid | msgstr |
|---|---|
| `IO Pressure (PSI) — Some` | `Presión de E/S (PSI) — Parcial` |
| `IO Pressure (PSI) — Full` | `Presión de E/S (PSI) — Completa` |
| `% of time tasks stalled waiting for I/O — some stall` | `% del tiempo que las tareas esperan E/S — stall parcial` |
| `% of time tasks stalled waiting for I/O — full stall` | `% del tiempo que las tareas esperan E/S — stall completo` |
| `IO Pressure Some avg10` | `Presión de E/S parcial avg10` |
| `IO Pressure Some avg60` | `Presión de E/S parcial avg60` |
| `IO Pressure Some avg300` | `Presión de E/S parcial avg300` |
| `IO Pressure Full avg10` | `Presión de E/S completa avg10` |
| `IO Pressure Full avg60` | `Presión de E/S completa avg60` |
| `IO Pressure Full avg300` | `Presión de E/S completa avg300` |
| `Triggers when 10s IO pressure some stall exceeds a predefined level` | `Se activa cuando la presión de E/S parcial de 10s supera un nivel predefinido` |
| `Triggers when 60s IO pressure some stall exceeds a predefined level` | `Se activa cuando la presión de E/S parcial de 60s supera un nivel predefinido` |
| `Triggers when 300s IO pressure some stall exceeds a predefined level` | `Se activa cuando la presión de E/S parcial de 300s supera un nivel predefinido` |
| `Triggers when 10s IO pressure full stall exceeds a predefined level` | `Se activa cuando la presión de E/S completa de 10s supera un nivel predefinido` |
| `Triggers when 60s IO pressure full stall exceeds a predefined level` | `Se activa cuando la presión de E/S completa de 60s supera un nivel predefinido` |
| `Triggers when 300s IO pressure full stall exceeds a predefined level` | `Se activa cuando la presión de E/S completa de 300s supera un nivel predefinido` |

Example of the exact edit pattern for the first row (same mechanical pattern for the other 15 — match on the `msgid` line and the immediately-following empty `msgstr ""` line):

```diff
 #: src/components/routes/system/charts/io-pressure-chart.tsx
 msgid "IO Pressure (PSI) — Some"
-msgstr ""
+msgstr "Presión de E/S (PSI) — Parcial"
```

- [ ] **Step 5: Compile catalogs**

Run: `cd internal/site && pnpm exec lingui compile && cd ../..`
Expected: no errors.

- [ ] **Step 6: Confirm no unrelated locale files changed beyond new empty entries**

Run: `git diff --stat internal/site/src/locales | tail -5`
Expected: all 30 `locales/*/*.po` files show small additions only. `es.po` is the only one with non-empty new `msgstr` values — spot check with:

Run: `git diff internal/site/src/locales/de/de.po | grep "^+msgstr" | grep -v '""'`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/site/src/locales
git commit -m "$(cat <<'EOF'
i18n: extract IO Pressure strings, add Spanish translations

Same pattern as Memory Pressure: strings are extracted into all 29
locale catalogs (empty msgstr, left for Crowdin) and hand-translated
only into Spanish.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 8: Clean up local frontend tooling artifacts again**

Run: `rm -rf internal/site/node_modules internal/site/pnpm-lock.yaml internal/site/pnpm-workspace.yaml`

---

### Task 14: End-to-end manual verification

**Files:** none (verification-only task)

**Interfaces:** none.

- [ ] **Step 1: Rebuild and redeploy the user's local Docker dev stack**

The same stack used to verify Memory Pressure: `supplemental/docker/same-system/docker-compose.dev.yml` (hub on port 8090, agent running in a real Linux container reporting real `/proc/pressure/*` values). Ensure `internal/site/node_modules`/`pnpm-lock.yaml`/`pnpm-workspace.yaml` are removed first (Task 12/13 Step 5/8) to avoid the Docker build's `COPY internal/site/ ./` symlink conflict seen during Memory Pressure.

```bash
cd supplemental/docker/same-system
docker compose -f docker-compose.dev.yml build
docker compose -f docker-compose.dev.yml up -d
```

- [ ] **Step 2: Verify deployment health**

```bash
docker compose -f docker-compose.dev.yml ps
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8090/api/health
docker logs beszel-agent --tail 10
```
Expected: both containers `Up`, health check `200`, agent log shows `WebSocket connected`.

- [ ] **Step 3: Ask the user to verify in-browser (or verify directly if browser tooling is available)**

Confirm:
- The Disk I/O chart's existing "view more" button still opens its panel normally (no regression to the pre-existing I/O Time/Queue Depth/I/O Await/Utilization sections).
- An "IO Pressure (PSI)" section (Some + Full cards) now appears in that panel for the root filesystem, with real (or gracefully-absent) data.
- If the system has any "extra filesystems" configured, confirm the IO Pressure section does **not** appear under an extra-filesystem's panel (only the root filesystem's).
- The alert configuration UI shows 6 new entries: "IO Pressure Some avg10/60/300" and "IO Pressure Full avg10/60/300", each with a level `Select` dropdown (Warning/High/Critical), matching the existing CPU/Memory Pressure alert entries' UI.

- [ ] **Step 4: Report findings**

If anything doesn't match, fix the relevant task's files, re-run the affected verification commands, rebuild/redeploy, and re-check before considering this task done.

---

## Self-Review Notes

- **Spec coverage:** §"Scope decisions" (naming, `!extraFsName` gating, shared helper) → Tasks 1-3, 11. §1 (agent collection) → Tasks 1, 2, 3, 5. §2 (data model) → Tasks 4, 6, 8. §3 (frontend components) → Tasks 10, 11. §4 (alerts) → Tasks 7, 9. §5 (i18n) → Task 13. §6 (verification) → Tasks 12, 14, plus targeted test runs embedded in Tasks 1, 4, 5, 7.
- **Type consistency verified:** `statsKey: "iodp" | "iodf"` (Task 10) matches the `SystemStats` field names added in Task 8. Alert key strings (`IOPressureSomeAvg10` etc.) are byte-identical between Task 7 (Go) and Task 9 (TypeScript). `IOPressureSome`/`IOPressureFull` field names are identical across Tasks 4, 6, 10. `readPressureFile`'s signature (Task 1) matches its call sites in Tasks 2 and 3 exactly.
- **No placeholders:** every task has complete, real code; the i18n task (13) has the full 16-row real-translation table instead of a "translate this" instruction.
- **Lesson carried forward from Memory Pressure's final review:** `omitzero` (not `omitempty`) is specified from the start for the new fixed-size array fields (Task 4), and the existing serialization regression test is extended rather than left to bit-rot (Task 4, Step 3) — this was the single hardest-to-catch bug in the prior feature and is now guarded going forward.
