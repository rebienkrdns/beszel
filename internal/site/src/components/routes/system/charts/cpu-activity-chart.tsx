import { t } from "@lingui/core/macro"
import AreaChartDefault from "@/components/charts/area-chart"
import { decimalString, toFixedFloat } from "@/lib/utils"
import type { ChartData } from "@/types"
import { ChartCard } from "../chart-card"

function fmtRate(val: number) {
	return val >= 1000 ? `${toFixedFloat(val / 1000, 1)}K/s` : `${decimalString(val)}/s`
}

export function CpuActivityChart({
	chartData,
	grid,
	dataEmpty,
}: {
	chartData: ChartData
	grid: boolean
	dataEmpty: boolean
}) {
	const latest = chartData.systemStats.at(-1)?.stats
	if (!latest || (latest.ctx === undefined && latest.irq === undefined)) {
		return null
	}

	return (
		<>
			{latest.ctx !== undefined && (
				<ChartCard
					empty={dataEmpty}
					grid={grid}
					className="min-h-auto"
					title={t`Context Switches`}
					description={t`Context switches per second`}
				>
					<AreaChartDefault
						chartData={chartData}
						dataPoints={[
							{
								label: t`Context Switches`,
								dataKey: ({ stats }) => stats?.ctx ?? 0,
								color: 1,
								opacity: 0.3,
							},
						]}
						tickFormatter={fmtRate}
						contentFormatter={(data) => fmtRate(data.value)}
					/>
				</ChartCard>
			)}
			{latest.irq !== undefined && (
				<ChartCard
					empty={dataEmpty}
					grid={grid}
					className="min-h-auto"
					title={t`Hardware Interrupts`}
					description={t`Hardware interrupts per second`}
				>
					<AreaChartDefault
						chartData={chartData}
						dataPoints={[
							{
								label: t`Interrupts`,
								dataKey: ({ stats }) => stats?.irq ?? 0,
								color: 3,
								opacity: 0.3,
							},
						]}
						tickFormatter={fmtRate}
						contentFormatter={(data) => fmtRate(data.value)}
					/>
				</ChartCard>
			)}
		</>
	)
}
