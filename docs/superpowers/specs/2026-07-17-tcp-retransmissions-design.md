# TCP retransmissions / network errors — design

Date: 2026-07-17
Status: Approved

## Summary

Add a new "Network Health" chart plus two independent threshold alerts covering two distinct
network-degradation signals that bandwidth alone doesn't show:

1. **TCP retransmissions/sec** — host-wide, Linux-only, from `/proc/net/snmp` (`Tcp:` line,
   `RetransSegs` field — the same counter `netstat -s` reports as "segments retransmited").
2. **Network errors+drops/sec** — interface-level, cross-platform, from gopsutil's
   `IOCountersStat.Errin/Errout/Dropin/Dropout`, aggregated across the same whitelisted/public
   NICs already used for the Bandwidth chart.

This is the third of the original 3-metric sequence agreed with the user
(MemAvailable → OOM Killer → **TCP retransmissions/network errors**), following its own
spec → plan → implementation cycle.

## Background / established conventions (from MemAvailable, OOM Killer)

- Backend: a new `/proc/*` file gets its own small reader file in `agent/` (`psi.go`, `oom.go`
  precedent). Cumulative counters that need a per-poll delta use a `map[uint16]uint64` baseline
  on the `Agent` struct, keyed by `cacheTimeMs`, with a `hasPrev && current >= prev` guard
  (handles both "first poll for this bucket" and "counter reset by reboot" — established in the
  OOM Killer shared-baseline-race fix).
- **Learned from MemAvailable and OOM Killer**: the `alerts.name` PocketBase `select` field in
  `internal/migrations/0_collections_snapshot_0_19_0_dev_1.go` has a closed enum — any new alert
  type name MUST be added there or no alert record with that name can ever be created (in tests
  or production). Included from the start this time.
- **Learned from OOM Killer**: whether a windowed alert should sum or average its accumulated
  value depends on whether the underlying `Stats` field is a continuous rate (average is
  correct — e.g. `Bandwidth`, `MemAvailable`) or a raw per-poll event count (sum is correct —
  e.g. `OOMKillDelta`). This feature's two new fields are **rates** (events/sec, not raw counts),
  confirmed by tracing `Bandwidth`'s existing windowed-alert handling (`alerts_system.go:297,
  386-397`), which already sums-then-divides (averages) a bytes/sec rate with no special case.
  Both new fields follow that same default-averaging path — no special-cased finalization
  needed, avoiding a repeat of the sum-vs-average bug found in OOM Killer.
- `omitzero` applies to both new `Stats` fields (0 is a normal, common, expected value for
  either — a healthy host legitimately shows 0 retransmissions/errors most of the time).

## Scope decisions specific to this feature

1. **Both signals in one feature** — TCP retransmissions (Linux-only, host-wide) and interface
   errors/drops (cross-platform, per-NIC-aggregated) are architecturally distinct but both
   represent "network degradation bandwidth doesn't show," and the second is nearly free to add
   once the first's per-poll infrastructure exists.
2. **Rates, not raw counts** — both fields are computed as events/sec (`delta * 1000 / msElapsed`),
   not raw per-poll deltas. This makes the chart's values independent of polling cadence (1s live
   view vs 60s recording) and lets alerts use the existing default-average windowed path with no
   special case (see Background).
3. **Aggregate only, no per-NIC breakdown** — network errors/drops are summed across all
   currently-valid NICs (same filter set as `Bandwidth`) into one total, not tracked per-interface.
   Matches `Bandwidth`'s own aggregate-total treatment; per-NIC error breakdown is deferred as
   unnecessary complexity for a "is my network degraded" signal.
4. **Single source for TCP retransmissions**: `/proc/net/snmp`'s `Tcp: RetransSegs`, not
   `/proc/net/netstat`'s `TcpExt:` block (which has ~15 retransmission-adjacent sub-counters).
   `RetransSegs` is the standard, well-known field used by `netstat -s` and tools like
   node_exporter's `node_netstat_Tcp_RetransSegs` — simplest single source, avoids an
   under-motivated choice among many similar sub-metrics.
5. **Two separate alert types**, not one combined "network degradation" alert — `TCPRetrans` and
   `NetworkErrors` are independent signals with independent thresholds, consistent with
   `MemAvailable`/`OOMKill` being separate types rather than merged.
6. **Dedicated chart, no info-bar counter** — unlike OOM Killer (rare event, meaningful lifetime
   total), both new metrics can accumulate large counts even on healthy systems, so a "since
   boot" info-bar counter would be a large, low-signal number. A historical chart (trend over
   time) is the right visualization instead; alerts cover the notification need.
7. **Chart auto-hides on absence, not on zero** — the new "Network Health" chart follows the
   existing `BatteryChart`/`TemperatureChart` convention (check whether the latest record has the
   field populated at all, not whether its value is `> 0`), since 0 is a valid healthy reading for
   both fields and using it as a hide-condition would incorrectly hide the chart on healthy Linux
   hosts. This accepts the same known imprecision already present in `BatteryChart`/
   `TemperatureChart` (a record that's genuinely all-zero also hides), not a new tradeoff.

## 1. Backend — agent collection

`agent/tcpstats.go` (new):

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
// non-Linux systems, if the file is unavailable, or if the field is missing.
func readTCPRetransSegs() uint64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	f, err := os.Open("/proc/net/snmp")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var fieldIndex = -1
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Tcp:") {
			continue
		}
		fields := strings.Fields(line)
		if fieldIndex == -1 {
			// this is the header line - find RetransSegs's position
			for i, name := range fields {
				if name == "RetransSegs" {
					fieldIndex = i
					break
				}
			}
			continue
		}
		// this is the values line - read the same position
		if fieldIndex <= 0 || fieldIndex >= len(fields) {
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

`agent/agent.go` — add to the `Agent` struct (near `prevOOMKillCount`):

```go
prevTCPRetransSegs map[uint16]uint64 // Previous cumulative TCP retransmit count per cache interval, for rate calculation
prevNetErrorsTotal map[uint16]uint64 // Previous cumulative network errors+drops total per cache interval, for rate calculation
```

`NewAgent()`:

```go
agent.prevTCPRetransSegs = make(map[uint16]uint64)
agent.prevNetErrorsTotal = make(map[uint16]uint64)
```

`agent/network.go` — extend `sumAndTrackPerNicDeltas` to also accumulate an errors+drops total
across the same already-filtered NICs (no new `DeltaTracker` keys — this is an aggregate, not a
per-NIC breakdown):

```go
func (a *Agent) sumAndTrackPerNicDeltas(cacheTimeMs uint16, msElapsed uint64, netIO []psutilNet.IOCountersStat, systemStats *system.Stats) (totalBytesSent, totalBytesRecv, totalErrors uint64) {
	...
	for _, v := range netIO {
		if _, exists := a.netInterfaces[v.Name]; !exists {
			continue
		}
		totalBytesSent += v.BytesSent
		totalBytesRecv += v.BytesRecv
		totalErrors += v.Errin + v.Errout + v.Dropin + v.Dropout
		...
	}
	return totalBytesSent, totalBytesRecv, totalErrors
}
```

`updateNetworkStats` — after the existing bandwidth calculation, using the same `msElapsed` from
`loadAndTickNetBaseline`:

```go
func (a *Agent) updateNetworkStats(cacheTimeMs uint16, systemStats *system.Stats) {
	...
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

		// TCP retransmissions live inside this same block so they can reuse msElapsed
		// from the network baseline above, rather than tracking their own timestamp.
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

`/proc/net/snmp` is read once per cache bucket per poll, same cadence as the NIC loop it's
nested alongside. If `psutilNet.IOCounters` fails, TCP-retransmission collection is skipped for
that poll too (consistent with every other network stat already being skipped in that case, and
simpler than maintaining a second independent baseline timestamp).

Both new counters guard with the same `hasPrev && current >= prev` pattern established for
`prevOOMKillCount` (handles first-poll-per-bucket and counter-reset-by-reboot identically), plus
an `msElapsed > 0` guard shared with the existing bandwidth rate calculation (avoids a
divide-by-zero-elapsed spike on the very first tick).

## 2. Data model

`internal/entities/system/system.go` — `Stats` struct, next free cbor keys (43, 44):

```go
TCPRetransPs    float64 `json:"trp,omitzero" cbor:"43,keyasint,omitzero"` // TCP retransmitted segments/sec (Linux only), /proc/net/snmp Tcp:RetransSegs
NetworkErrorsPs float64 `json:"nep,omitzero" cbor:"44,keyasint,omitzero"` // network interface errors+drops/sec, summed across public interfaces
```

`internal/alerts/alerts.go` — `SystemAlertStats`, mirrored fields:

```go
TCPRetransPs    float64 `json:"trp"`
NetworkErrorsPs float64 `json:"nep"`
```

`internal/site/src/types.d.ts` — `SystemStats`:

```ts
/** TCP retransmitted segments/sec (Linux only) */
trp?: number
/** network interface errors+drops/sec */
nep?: number
```

## 3. Records aggregation

`internal/records/records.go`, `AverageSystemStatsSlice`: both fields are rates, not event
counts — **averaged**, same treatment as `MemAvailable`/`NetworkSent`/`NetworkRecv`:

- Accumulation: `sum.TCPRetransPs += stats.TCPRetransPs`, `sum.NetworkErrorsPs += stats.NetworkErrorsPs`.
- Division ("Compute averages"): `sum.TCPRetransPs = twoDecimals(sum.TCPRetransPs / count)`,
  `sum.NetworkErrorsPs = twoDecimals(sum.NetworkErrorsPs / count)`.

## 4. Alerts

`internal/site/src/lib/alerts.ts` — new entries (near `Bandwidth`):

```ts
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

(`RefreshCwIcon`/`UnplugIcon` confirmed available as `Icon`-suffixed exports in the installed
`lucide-react` version.)

`internal/alerts/alerts_system.go`:
- Instant-check switch: `case "TCPRetrans": val = data.Stats.TCPRetransPs; unit = "/s"` and
  `case "NetworkErrors": val = data.Stats.NetworkErrorsPs; unit = "/s"`. No zero-guard (0 is a
  normal, common healthy value, and since these are averaged rates there's no "stuck alert" risk
  the way a `continue`-on-zero guard caused for `OOMKill`).
- Windowed accumulator switch: `case "TCPRetrans": alert.val += stats.TCPRetransPs` and
  `case "NetworkErrors": alert.val += stats.NetworkErrorsPs`.
- Finalization switch: **no special case** — both fall through to the existing
  `default: alert.val = alert.val / float64(alert.count)`, identical to `Bandwidth`.
- No `isLowAlert` change — both are normal (non-inverted) alerts, like `Bandwidth`/`CPU`.
- No special-cased subject/body wording — the generic "above/below threshold, averaged over N
  minutes" phrasing is correct for a continuous rate (unlike `OOMKill`'s discrete-event wording).

`internal/migrations/0_collections_snapshot_0_19_0_dev_1.go`: add `"TCPRetrans"` and
`"NetworkErrors"` to the `alerts.name` select field's `values` enum (required from the start).

## 5. Frontend UI — dedicated chart

New component `NetworkHealthChart` in
`internal/site/src/components/routes/system/charts/network-charts.tsx`, alongside
`BandwidthChart`:

```tsx
export function NetworkHealthChart({ chartData, grid, dataEmpty, showMax, isLongerChart, maxValues }: {
	chartData: ChartData
	grid: boolean
	dataEmpty: boolean
	showMax: boolean
	isLongerChart: boolean
	maxValues: boolean
}) {
	const showChart = chartData.systemStats.at(-1)?.stats.trp !== undefined || chartData.systemStats.at(-1)?.stats.nep !== undefined
	if (!showChart) {
		return null
	}
	return (
		<ChartCard
			empty={dataEmpty}
			grid={grid}
			title={t`Network Health`}
			description={t`TCP retransmissions and network interface errors`}
		>
			<AreaChartDefault
				chartData={chartData}
				dataPoints={[
					{ label: t`TCP Retransmissions`, dataKey: ({ stats }) => stats?.trp, color: 3, opacity: 0.3 },
					{ label: t`Errors + Drops`, dataKey: ({ stats }) => stats?.nep, color: 4, opacity: 0.3 },
				]}
				tickFormatter={(val) => `${toFixedFloat(val, val >= 10 ? 0 : 1)}/s`}
				contentFormatter={(data) => `${decimalString(data.value)}/s`}
			/>
		</ChartCard>
	)
}
```

Registered in `internal/site/src/components/routes/system.tsx`, alongside `<BandwidthChart>`
(both occurrences, ~line 108 and ~189), same `coreProps`.

## 6. i18n

New `t\`...\`` strings: 2 alert names, 2 alert descriptions, chart title, chart description, 2
chart series labels. Extracted via lingui to all 30 locales (empty `msgstr`), hand-translated
only into Spanish (`es.po`), matching the MemAvailable/OOM Killer precedent. "TCP" is left
untranslated (standard networking term, matching how "OOM Killer"/"swap"/"kernel" were left
untranslated).

## 7. Verification

- Backend: `go build ./agent/... ./internal/entities/... ./internal/alerts/... ./internal/records/... ./internal/migrations/...`;
  `go test -tags=testing ./agent/... ./internal/entities/... ./internal/records/... ./internal/alerts/...`.
- New tests:
  - `agent/tcpstats_test.go`: parses a synthetic `/proc/net/snmp`-format fixture, asserting
    correct field-index resolution; missing-field and non-Linux-GOOS cases return 0.
  - Rate-calculation test for the new NIC-errors aggregation (first poll, steady state, counter
    reset) — mirrors the reasoning already verified for `prevOOMKillCount`'s `hasPrev` guard.
  - `internal/entities/system/system_test.go`: extend the shared `omitzero`-fields-omitted/present
    regression test with `"trp"`/`"nep"`, same as the `MemAvailable`/`OOMKillDelta` precedent.
  - `internal/records/records_averaging_test.go`: assert both fields average (not sum) across a
    multi-record slice.
  - `internal/alerts/alerts_system_test.go`: setters for both fields, added to the existing
    `TestSystemAlertsOneMin`/`TestSystemAlertsTwoMin` tables (reusing the shared multi-minute
    helper as-is, since both fields average like `CPU`/`Bandwidth` — no bespoke test needed the
    way `OOMKill`'s sum-not-average behavior required one).
- Frontend: `bunx tsc -b` clean; visual check on the user's local Docker dev stack
  (`supplemental/docker/same-system/docker-compose.dev.yml`) confirming the "Network Health"
  chart renders (or correctly stays hidden pre-data) and both new alert types appear in alert
  configuration with correct descriptions/units.

## Out of scope

- Per-NIC error/drop breakdown (aggregate total only, matching `Bandwidth`).
- Granular `/proc/net/netstat` `TcpExt:` sub-metrics (`TCPFastRetrans`, `TCPSynRetrans`, etc.) —
  only the single `RetransSegs` total.
- Per-container network-error alerting (no container-level alerting mechanism exists anywhere in
  this codebase today — confirmed via `grep` during this feature's exploration phase).
- Any info-bar counter for either metric.
- The pre-existing PSI alert migration-enum gap (flagged during MemAvailable, still unfixed) —
  unrelated to this feature, left as a standing follow-up recommendation.
