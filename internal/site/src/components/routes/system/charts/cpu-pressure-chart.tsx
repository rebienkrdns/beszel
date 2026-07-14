import { t } from "@lingui/core/macro"
import LineChartDefault from "@/components/charts/line-chart"
import { toFixedFloat, cn } from "@/lib/utils"
import type { ChartData, SystemStatsRecord } from "@/types"
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import Spinner from "@/components/spinner"
import { useIntersectionObserver } from "@/lib/use-intersection-observer"

function pressureColor(value: number): string {
	if (value < 1) return "hsl(142, 71%, 45%)"
	if (value < 2) return "hsl(142, 60%, 55%)"
	if (value < 5) return "hsl(48, 96%, 45%)"
	if (value < 10) return "hsl(25, 95%, 53%)"
	return "hsl(0, 72%, 51%)"
}

function pressureLabel(value: number): string {
	if (value < 1) return t`Excellent`
	if (value < 2) return t`Normal`
	if (value < 5) return t`Warning`
	if (value < 10) return t`High`
	return t`Critical`
}

function PressureBadge({ label, value }: { label: string; value: number }) {
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
