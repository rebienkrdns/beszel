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
