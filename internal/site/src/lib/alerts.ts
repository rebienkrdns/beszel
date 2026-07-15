import { t } from "@lingui/core/macro"
import { CpuIcon, GaugeIcon, HardDriveIcon, MemoryStickIcon, ServerIcon } from "lucide-react"
import type { RecordSubscription } from "pocketbase"
import { EthernetIcon, GpuIcon } from "@/components/ui/icons"
import { $alerts } from "@/lib/stores"
import type { AlertInfo, AlertRecord } from "@/types"
import { pb } from "./api"
import { ThermometerIcon, BatteryMediumIcon, HourglassIcon } from "@/components/ui/icons"

/** Alert info for each alert type */
export const alertInfo: Record<string, AlertInfo> = {
	Status: {
		name: () => t`Status`,
		unit: "",
		icon: ServerIcon,
		desc: () => t`Triggers when status switches between up and down`,
		/** "for x minutes" is appended to desc when only one value */
		singleDesc: () => `${t`System`} ${t`Down`}`,
	},
	CPU: {
		name: () => t`CPU Usage`,
		unit: "%",
		icon: CpuIcon,
		desc: () => t`Triggers when CPU usage exceeds a threshold`,
	},
	Memory: {
		name: () => t`Memory Usage`,
		unit: "%",
		icon: MemoryStickIcon,
		desc: () => t`Triggers when memory usage exceeds a threshold`,
	},
	Disk: {
		name: () => t`Disk Usage`,
		unit: "%",
		icon: HardDriveIcon,
		desc: () => t`Triggers when usage of any disk exceeds a threshold`,
	},
	Bandwidth: {
		name: () => t`Bandwidth`,
		unit: " MB/s",
		icon: EthernetIcon,
		desc: () => t`Triggers when combined up/down exceeds a threshold`,
		max: 250,
	},
	GPU: {
		name: () => t`GPU Usage`,
		unit: "%",
		icon: GpuIcon,
		desc: () => t`Triggers when GPU usage exceeds a threshold`,
	},
	Temperature: {
		name: () => t`Temperature`,
		unit: "°C",
		icon: ThermometerIcon,
		desc: () => t`Triggers when any sensor exceeds a threshold`,
	},
	LoadAvg1: {
		name: () => t`Load Average 1m`,
		unit: "",
		icon: HourglassIcon,
		max: 100,
		min: 0.1,
		start: 10,
		step: 0.1,
		desc: () => t`Triggers when 1 minute load average exceeds a threshold`,
	},
	LoadAvg5: {
		name: () => t`Load Average 5m`,
		unit: "",
		icon: HourglassIcon,
		max: 100,
		min: 0.1,
		start: 10,
		step: 0.1,
		desc: () => t`Triggers when 5 minute load average exceeds a threshold`,
	},
	LoadAvg15: {
		name: () => t`Load Average 15m`,
		unit: "",
		icon: HourglassIcon,
		min: 0.1,
		max: 100,
		start: 10,
		step: 0.1,
		desc: () => t`Triggers when 15 minute load average exceeds a threshold`,
	},
	Battery: {
		name: () => t`Battery`,
		unit: "%",
		icon: BatteryMediumIcon,
		desc: () => t`Triggers when battery charge drops below a threshold`,
		start: 20,
		invert: true,
	},
	CpuPressureAvg10: {
		name: () => t`CPU Pressure avg10`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 10s CPU pressure stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
	CpuPressureAvg60: {
		name: () => t`CPU Pressure avg60`,
		unit: "%",
		icon: GaugeIcon,
		desc: () => t`Triggers when 60s CPU pressure stall exceeds a predefined level`,
		levels: [
			{ label: () => t`Warning (>2%)`, value: 2 },
			{ label: () => t`High (>5%)`, value: 5 },
			{ label: () => t`Critical (>10%)`, value: 10 },
		],
	},
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

/** Helper to manage user alerts */
export const alertManager = (() => {
	const collection = pb.collection<AlertRecord>("alerts")
	let unsub: () => void

	/** Fields to fetch from alerts collection */
	const fields = "id,name,system,value,min,triggered"

	/** Fetch alerts from collection */
	async function fetchAlerts(): Promise<AlertRecord[]> {
		return await collection.getFullList<AlertRecord>({ fields, sort: "updated" })
	}

	/** Format alerts into a map of system id to alert name to alert record */
	function add(alerts: AlertRecord[]) {
		for (const alert of alerts) {
			const systemId = alert.system
			const systemAlerts = $alerts.get()[systemId] ?? new Map()
			const newAlerts = new Map(systemAlerts)
			newAlerts.set(alert.name, alert)
			$alerts.setKey(systemId, newAlerts)
		}
	}

	function remove(alerts: Pick<AlertRecord, "name" | "system">[]) {
		for (const alert of alerts) {
			const systemId = alert.system
			const systemAlerts = $alerts.get()[systemId]
			const newAlerts = new Map(systemAlerts)
			newAlerts.delete(alert.name)
			$alerts.setKey(systemId, newAlerts)
		}
	}

	const actionFns = {
		create: add,
		update: add,
		delete: remove,
	}

	// batch alert updates to prevent unnecessary re-renders when adding many alerts at once
	const batchUpdate = (() => {
		const batch = new Map<string, RecordSubscription<AlertRecord>>()
		let timeout: ReturnType<typeof setTimeout>

		return (data: RecordSubscription<AlertRecord>) => {
			const { record } = data
			batch.set(`${record.system}${record.name}`, data)
			clearTimeout(timeout)
			timeout = setTimeout(() => {
				const groups = { create: [], update: [], delete: [] } as Record<string, AlertRecord[]>
				for (const { action, record } of batch.values()) {
					groups[action]?.push(record)
				}
				for (const key in groups) {
					if (groups[key].length) {
						actionFns[key as keyof typeof actionFns]?.(groups[key])
					}
				}
				batch.clear()
			}, 50)
		}
	})()

	async function subscribe() {
		unsub = await collection.subscribe("*", batchUpdate, { fields })
	}

	function unsubscribe() {
		unsub?.()
	}

	async function refresh() {
		const records = await fetchAlerts()
		add(records)
	}

	return {
		/** Add alerts to store */
		add,
		/** Remove alerts from store */
		remove,
		/** Subscribe to alerts */
		subscribe,
		/** Unsubscribe from alerts */
		unsubscribe,
		/** Refresh alerts with latest data from hub */
		refresh,
	}
})()
