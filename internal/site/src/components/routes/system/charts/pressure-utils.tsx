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
