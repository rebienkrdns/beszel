import { t } from "@lingui/core/macro"
import { Trans } from "@lingui/react/macro"
import { BellIcon, LoaderCircleIcon, PlusIcon, SaveIcon, Trash2Icon } from "lucide-react"
import { type ChangeEventHandler, useEffect, useState } from "react"
import * as v from "valibot"
import { prependBasePath } from "@/components/router"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { InputTags } from "@/components/ui/input-tags"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, pb } from "@/lib/api"
import type { UserSettings } from "@/types"
import { saveSettings } from "./layout"
import { QuietHours } from "./quiet-hours"
import type { ClientResponseError } from "pocketbase"

interface ShoutrrrUrlCardProps {
	url: string
	onUrlChange: ChangeEventHandler<HTMLInputElement>
	onRemove: () => void
}

const NotificationSchema = v.object({
	emails: v.array(v.pipe(v.string(), v.rfcEmail())),
	webhooks: v.array(v.pipe(v.string(), v.url())),
})

const SettingsNotificationsPage = ({ userSettings }: { userSettings: UserSettings }) => {
	const [webhooks, setWebhooks] = useState(userSettings.webhooks ?? [])
	const [emails, setEmails] = useState<string[]>(userSettings.emails ?? [])
	const [isLoading, setIsLoading] = useState(false)

	// update values when userSettings changes
	useEffect(() => {
		setWebhooks(userSettings.webhooks ?? [])
		setEmails(userSettings.emails ?? [])
	}, [userSettings])

	function addWebhook() {
		setWebhooks([...webhooks, ""])
		// focus on the new input
		queueMicrotask(() => {
			const inputs = document.querySelectorAll("#webhooks input") as NodeListOf<HTMLInputElement>
			inputs[inputs.length - 1]?.focus()
		})
	}
	const removeWebhook = (index: number) => setWebhooks(webhooks.filter((_, i) => i !== index))

	function updateWebhook(index: number, value: string) {
		const newWebhooks = [...webhooks]
		newWebhooks[index] = value
		setWebhooks(newWebhooks)
	}

	async function updateSettings() {
		setIsLoading(true)
		try {
			const parsedData = v.parse(NotificationSchema, { emails, webhooks })
			await saveSettings(parsedData)
		} catch (e: unknown) {
			toast({
				title: t`Failed to save settings`,
				description: (e as Error).message,
				variant: "destructive",
			})
		}
		setIsLoading(false)
	}

	return (
		<div>
			<div>
				<h3 className="text-xl font-medium mb-2">
					<Trans>Notifications</Trans>
				</h3>
				<p className="text-sm text-muted-foreground leading-relaxed">
					<Trans>Configure how you receive alert notifications.</Trans>
				</p>
				<p className="text-sm text-muted-foreground mt-1.5 leading-relaxed">
					<Trans>
						Looking instead for where to create alerts? Click the bell <BellIcon className="inline h-4 w-4" /> icons in
						the systems table.
					</Trans>
				</p>
			</div>
			<Separator className="my-4" />
			<div className="space-y-5">
				<div className="grid gap-2">
					<div className="mb-2">
						<h3 className="mb-1 text-lg font-medium">
							<Trans>Email notifications</Trans>
						</h3>
						{isAdmin() && <MailProviderSettings />}
					</div>
					<Label className="block" htmlFor="email">
						<Trans>To email(s)</Trans>
					</Label>
					<InputTags
						value={emails}
						onChange={setEmails}
						placeholder={t`Enter email address...`}
						className="w-full"
						type="email"
						id="email"
					/>
					<p className="text-[0.8rem] text-muted-foreground">
						<Trans>Save address using enter key or comma. Leave blank to disable email notifications.</Trans>
					</p>
				</div>
				<Separator />
				<div className="space-y-3">
					<div className="grid grid-cols-1 sm:flex items-center justify-between gap-4">
						<div>
							<h3 className="mb-1 text-lg font-medium">
								<Trans>Webhook / Push notifications</Trans>
							</h3>
							<p className="text-sm text-muted-foreground leading-relaxed">
								<Trans>
									Beszel uses{" "}
									<a href="https://beszel.dev/guide/notifications" target="_blank" className="link" rel="noopener">
										Shoutrrr
									</a>{" "}
									to integrate with popular notification services.
								</Trans>
							</p>
							<p className="text-sm text-muted-foreground mt-1 leading-relaxed">
								<Trans>
									Example for Discord:{" "}
									<code className="bg-muted rounded-sm px-1 text-primary">discord://TOKEN@WEBHOOK_ID</code> — get{" "}
									<code className="bg-muted rounded-sm px-1 text-primary">TOKEN</code> and{" "}
									<code className="bg-muted rounded-sm px-1 text-primary">WEBHOOK_ID</code> from your Discord webhook
									URL (
									<code className="bg-muted rounded-sm px-1 text-primary">
										https://discord.com/api/webhooks/WEBHOOK_ID/TOKEN
									</code>
									).
								</Trans>
							</p>
						</div>
						<Button type="button" variant="outline" className="h-10 shrink-0" onClick={addWebhook}>
							<PlusIcon className="size-4" />
							<span className="ms-1">
								<Trans>Add URL</Trans>
							</span>
						</Button>
					</div>
					{webhooks.length > 0 && (
						<div className="grid gap-2.5" id="webhooks">
							{webhooks.map((webhook, index) => (
								<ShoutrrrUrlCard
									key={index}
									url={webhook}
									onUrlChange={(e: React.ChangeEvent<HTMLInputElement>) => updateWebhook(index, e.target.value)}
									onRemove={() => removeWebhook(index)}
								/>
							))}
						</div>
					)}
				</div>
				<Separator />
				<div className="space-y-3">
					<QuietHours />
				</div>
				<Separator />
				<Button
					type="button"
					className="flex items-center gap-1.5 disabled:opacity-100"
					onClick={updateSettings}
					disabled={isLoading}
				>
					{isLoading ? <LoaderCircleIcon className="h-4 w-4 animate-spin" /> : <SaveIcon className="h-4 w-4" />}
					<Trans>Save Settings</Trans>
				</Button>
			</div>
		</div>
	)
}

function showTestNotificationError(msg: string) {
	toast({
		title: t`Error`,
		description: msg ?? t`Failed to send test notification`,
		variant: "destructive",
	})
}

const ShoutrrrUrlCard = ({ url, onUrlChange, onRemove }: ShoutrrrUrlCardProps) => {
	const [isLoading, setIsLoading] = useState(false)

	const sendTestNotification = async () => {
		setIsLoading(true)
		try {
			const res = await pb.send("/api/beszel/test-notification", { method: "POST", body: { url } })
			if ("err" in res && !res.err) {
				toast({
					title: t`Test notification sent`,
					description: t`Check your notification service`,
				})
			} else {
				showTestNotificationError(res.err)
			}
		} catch (e: unknown) {
			showTestNotificationError((e as ClientResponseError).data?.message)
		} finally {
			setIsLoading(false)
		}
	}

	return (
		<Card className="bg-table-header p-2 md:p-3">
			<div className="flex items-center gap-1">
				<Input
					type="url"
					className="light:bg-card"
					required
					placeholder="generic://webhook.site/xxxxxx"
					value={url}
					onChange={onUrlChange}
				/>
				<Button type="button" variant="outline" disabled={isLoading || url === ""} onClick={sendTestNotification}>
					{isLoading ? (
						<LoaderCircleIcon className="h-4 w-4 animate-spin" />
					) : (
						<span>
							<Trans>
								Test <span className="hidden sm:inline">URL</span>
							</Trans>
						</span>
					)}
				</Button>
				<Button type="button" variant="outline" size="icon" className="shrink-0" aria-label="Delete" onClick={onRemove}>
					<Trash2Icon className="h-4 w-4" />
				</Button>
			</div>
		</Card>
	)
}

interface MailSettingsInfo {
	provider: "smtp" | "resend"
	providerSource: "env" | "db"
	hasResendApiKey: boolean
	resendApiKeySource: "env" | "db" | "none"
}

const MailProviderSettings = () => {
	const [info, setInfo] = useState<MailSettingsInfo | null>(null)
	const [provider, setProvider] = useState<"smtp" | "resend">("smtp")
	const [resendApiKey, setResendApiKey] = useState("")
	const [isLoading, setIsLoading] = useState(true)
	const [isSaving, setIsSaving] = useState(false)

	useEffect(() => {
		fetchInfo()
	}, [])

	async function fetchInfo() {
		try {
			setIsLoading(true)
			const res = await pb.send<MailSettingsInfo>("/api/beszel/mail-settings", {})
			setInfo(res)
			setProvider(res.provider)
		} catch (e: unknown) {
			toast({
				title: t`Error`,
				description: (e as Error).message,
				variant: "destructive",
			})
		} finally {
			setIsLoading(false)
		}
	}

	async function save() {
		setIsSaving(true)
		try {
			await pb.send("/api/beszel/mail-settings", {
				method: "POST",
				body: { provider, resendApiKey },
			})
			setResendApiKey("")
			await fetchInfo()
			toast({ title: t`Mail provider settings saved` })
		} catch (e: unknown) {
			toast({
				title: t`Failed to save mail provider settings`,
				description: (e as Error).message,
				variant: "destructive",
			})
		} finally {
			setIsSaving(false)
		}
	}

	if (isLoading || !info) {
		return null
	}

	const envLocked = info.providerSource === "env" || info.resendApiKeySource === "env"

	if (envLocked) {
		return (
			<div className="grid gap-2">
				<p className="text-sm text-muted-foreground leading-relaxed">
					<Trans>
						Mail provider is controlled by environment variables on this hub (
						<code className="bg-muted rounded-sm px-1 text-primary">BESZEL_HUB_MAIL_PROVIDER</code> /{" "}
						<code className="bg-muted rounded-sm px-1 text-primary">BESZEL_HUB_RESEND_API_KEY</code>).
					</Trans>
				</p>
				<p className="text-sm text-muted-foreground leading-relaxed">
					<Trans>Active provider:</Trans> <span className="font-medium">{info.provider}</span>
				</p>
			</div>
		)
	}

	return (
		<div className="grid gap-3">
			<div className="grid gap-2 max-w-xs">
				<Label htmlFor="mail-provider">
					<Trans>Mail provider</Trans>
				</Label>
				<Select value={provider} onValueChange={(value: "smtp" | "resend") => setProvider(value)}>
					<SelectTrigger id="mail-provider">
						<SelectValue />
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="smtp">SMTP</SelectItem>
						<SelectItem value="resend">Resend</SelectItem>
					</SelectContent>
				</Select>
			</div>
			{provider === "smtp" && (
				<p className="text-sm text-muted-foreground leading-relaxed">
					<Trans>
						Please{" "}
						<a href={prependBasePath("/_/#/settings/mail")} className="link" target="_blank">
							configure an SMTP server
						</a>{" "}
						to ensure alerts are delivered.
					</Trans>
				</p>
			)}
			{provider === "resend" && (
				<div className="grid gap-2 max-w-xs">
					<Label htmlFor="resend-api-key">
						<Trans>Resend API key</Trans>
					</Label>
					<Input
						id="resend-api-key"
						type="password"
						value={resendApiKey}
						onChange={(e) => setResendApiKey(e.target.value)}
						placeholder={info.hasResendApiKey ? "•••••••• (already set)" : t`Enter Resend API key...`}
					/>
					<p className="text-[0.8rem] text-muted-foreground">
						<Trans>Leave blank to keep the currently saved key.</Trans>
					</p>
				</div>
			)}
			<Button type="button" variant="outline" className="w-fit" onClick={save} disabled={isSaving}>
				{isSaving ? <LoaderCircleIcon className="h-4 w-4 animate-spin" /> : <SaveIcon className="h-4 w-4" />}
				<span className="ms-1">
					<Trans>Save mail provider</Trans>
				</span>
			</Button>
		</div>
	)
}

export default SettingsNotificationsPage
