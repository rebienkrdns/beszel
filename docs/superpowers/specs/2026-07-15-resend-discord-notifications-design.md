# Resend + Discord notification providers — design

Date: 2026-07-15
Status: Approved

## Summary

Add [Resend](https://resend.com) as a second email-delivery provider alongside the existing PocketBase SMTP mailer, selectable by the admin at the instance level (not per-user). Discord already works today through the existing per-user "Webhook / Push notifications" field (a Shoutrrr URL, e.g. `discord://TOKEN@WEBHOOK_ID`) — no backend change is needed there, only a documentation/example note in the UI so admins/users know the expected format without reading the Shoutrrr docs.

Scope decisions (from user Q&A during brainstorming):
1. **Discord**: keep the existing generic-webhook mechanism as-is. Only add an example note in the UI. No dedicated Discord input field, no backend change.
2. **Resend is instance-wide, not per-user** — same model as SMTP today (configured once, applies to all users' email alerts).
3. **Configurable both ways**: via `BESZEL_HUB_` environment variables (matching the existing `utils.GetEnv` prefix convention used by `AUTO_LOGIN`, `APP_URL`, etc.) **and** via a UI/DB-backed setting. **Environment variable wins** when both are present, for each of (a) the active provider and (b) the Resend API key, independently.
4. **New `hub_settings` singleton collection** for the UI/DB path, since PocketBase's built-in `Settings()` struct (used for SMTP config today, edited at `/_/#/settings/mail`) is a fixed framework struct and cannot be extended with custom fields like a Resend API key without forking PocketBase.
5. **UI lives inside Settings > Notifications** (`notifications.tsx`), visible only to `isAdmin()` users, replacing the current "please configure an SMTP server" hint.
6. **Resend HTTP client implemented directly with `net/http`**, no new SDK dependency — Resend's send-email API is a single JSON POST request.

## 1. Backend — `hub_settings` collection

Added to `internal/migrations/0_collections_snapshot_0_19_0_dev_1.go`, in the same JSON-array style as every other collection in that file (see `user_settings` for the closest precedent — a settings blob owned by a single, restricted subject):

```json
{
	"id": "hubmailsett1ngs",
	"listRule": null,
	"viewRule": null,
	"createRule": null,
	"updateRule": null,
	"deleteRule": null,
	"name": "hub_settings",
	"type": "base",
	"fields": [
		{
			"autogeneratePattern": "[a-z0-9]{15}",
			"hidden": false,
			"id": "text3208210256",
			"max": 15,
			"min": 15,
			"name": "id",
			"pattern": "^[a-z0-9]+$",
			"presentable": false,
			"primaryKey": true,
			"required": true,
			"system": true,
			"type": "text"
		},
		{
			"hidden": false,
			"id": "sel1mailprov",
			"maxSelect": 1,
			"name": "mail_provider",
			"presentable": false,
			"required": true,
			"system": false,
			"type": "select",
			"values": ["smtp", "resend"]
		},
		{
			"hidden": false,
			"id": "txt1resendkey",
			"max": 0,
			"min": 0,
			"name": "resend_api_key",
			"presentable": false,
			"required": false,
			"system": false,
			"type": "text"
		},
		{
			"hidden": false,
			"id": "autodate2990389176",
			"name": "created",
			"onCreate": true,
			"onUpdate": false,
			"presentable": false,
			"system": false,
			"type": "autodate"
		},
		{
			"hidden": false,
			"id": "autodate3332085495",
			"name": "updated",
			"onCreate": true,
			"onUpdate": true,
			"presentable": false,
			"system": false,
			"type": "autodate"
		}
	],
	"indexes": [],
	"system": false
}
```

All five rules are `null` (superuser-only via the generic REST API). App-level `"admin"`-role users never touch this collection directly — they go exclusively through the two custom routes below, which mask the API key on read and enforce `requireAdminRole`. This mirrors the hardening direction already established in this codebase (`565162ef refactor(hub): harden/enforce pb api rules and add tests`).

There is no data-seeding migration step — the single row is created lazily ("get or create") the first time it's needed, avoiding a separate migration function.

## 2. Backend — mail settings resolution

New file `internal/alerts/mail_settings.go`:

```go
package alerts

import (
	"database/sql"
	"errors"

	"github.com/henrygd/beszel/internal/hub/utils"
	"github.com/pocketbase/pocketbase/core"
)

const hubSettingsCollection = "hub_settings"

// MailSettingsInfo is the client-facing view of the resolved mail configuration.
// ResendApiKey itself is never included - only whether one is configured and where it came from.
type MailSettingsInfo struct {
	Provider           string `json:"provider"`
	ProviderSource     string `json:"providerSource"`     // "env" | "db"
	HasResendApiKey    bool   `json:"hasResendApiKey"`
	ResendApiKeySource string `json:"resendApiKeySource"` // "env" | "db" | "none"
}

// getOrCreateHubSettings returns the singleton hub_settings record, creating it
// with defaults (mail_provider: "smtp") the first time it's requested.
func getOrCreateHubSettings(app core.App) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(hubSettingsCollection, "")
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	collection, err := app.FindCachedCollectionByNameOrId(hubSettingsCollection)
	if err != nil {
		return nil, err
	}
	record = core.NewRecord(collection)
	record.Set("mail_provider", "smtp")
	if err := app.Save(record); err != nil {
		return nil, err
	}
	return record, nil
}

// resolveMailSettings resolves the effective mail provider and Resend API key,
// giving precedence to BESZEL_HUB_ environment variables over the stored
// hub_settings record. Provider and API key are resolved independently.
func resolveMailSettings(app core.App) (info MailSettingsInfo, resendApiKey string, err error) {
	record, err := getOrCreateHubSettings(app)
	if err != nil {
		return MailSettingsInfo{}, "", err
	}

	if envProvider, ok := utils.GetEnv("MAIL_PROVIDER"); ok && envProvider != "" {
		info.Provider, info.ProviderSource = envProvider, "env"
	} else {
		info.Provider, info.ProviderSource = record.GetString("mail_provider"), "db"
	}

	if envKey, ok := utils.GetEnv("RESEND_API_KEY"); ok && envKey != "" {
		resendApiKey, info.ResendApiKeySource = envKey, "env"
	} else if dbKey := record.GetString("resend_api_key"); dbKey != "" {
		resendApiKey, info.ResendApiKeySource = dbKey, "db"
	} else {
		info.ResendApiKeySource = "none"
	}
	info.HasResendApiKey = info.ResendApiKeySource != "none"

	return info, resendApiKey, nil
}
```

## 3. Backend — Resend mail client

New file `internal/alerts/mail_resend.go`:

```go
package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"

	"github.com/pocketbase/pocketbase/tools/mailer"
)

const resendApiUrl = "https://api.resend.com/emails"

// ResendMailer sends email through the Resend HTTP API. It implements the
// same mailer.Mailer interface as PocketBase's built-in SMTP/Sendmail clients,
// so it's a drop-in alternative wherever a mailer.Mailer is expected.
type ResendMailer struct {
	ApiKey string
}

type resendEmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	Html    string   `json:"html,omitempty"`
}

func (r *ResendMailer) Send(message *mailer.Message) error {
	body, err := json.Marshal(resendEmailRequest{
		From:    message.From.String(),
		To:      addressList(message.To),
		Subject: message.Subject,
		Text:    message.Text,
		Html:    message.HTML,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, resendApiUrl, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.ApiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend api error: status %d", resp.StatusCode)
	}
	return nil
}

func addressList(addrs []mail.Address) []string {
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = a.Address
	}
	return out
}
```

## 4. Backend — wire into `SendAlert`

`internal/alerts/alerts.go`, replace the fixed `am.hub.NewMailClient()` call with a resolved client:

```go
mailClient, err := am.resolveMailClient()
if err != nil {
	am.hub.Logger().Error("Failed to resolve mail client", "err", err)
	return err
}
err = mailClient.Send(&message)
```

New method on `*AlertManager`:

```go
// resolveMailClient returns the mailer.Mailer to use for the currently
// configured provider (SMTP or Resend), applying env-over-DB precedence.
func (am *AlertManager) resolveMailClient() (mailer.Mailer, error) {
	info, resendApiKey, err := resolveMailSettings(am.hub)
	if err != nil {
		return nil, err
	}
	if info.Provider == "resend" {
		if resendApiKey == "" {
			return nil, fmt.Errorf("resend is selected as the mail provider but no API key is configured")
		}
		return &ResendMailer{ApiKey: resendApiKey}, nil
	}
	return am.hub.NewMailClient(), nil
}
```

If Resend is selected without a key configured anywhere, the email leg of `SendAlert` fails loudly (logged, error returned) rather than silently falling back to SMTP — a misconfigured Resend selection shouldn't masquerade as a working SMTP send.

## 5. Backend — admin API routes

New route handlers in `internal/alerts/alerts_api.go` (same file/style as `UpsertUserAlerts`/`DeleteUserAlerts`):

```go
// GetMailSettings returns the effective mail provider configuration.
// (GET /api/beszel/mail-settings, admin role required)
func GetMailSettings(e *core.RequestEvent) error {
	info, _, err := resolveMailSettings(e.App)
	if err != nil {
		return err
	}
	return e.JSON(http.StatusOK, info)
}

// UpdateMailSettings updates the stored mail provider and/or Resend API key.
// (POST /api/beszel/mail-settings, admin role required)
func UpdateMailSettings(e *core.RequestEvent) error {
	var data struct {
		Provider     string `json:"provider"`
		ResendApiKey string `json:"resendApiKey"`
	}
	if err := e.BindBody(&data); err != nil {
		return e.BadRequestError("Bad data", err)
	}
	if data.Provider != "smtp" && data.Provider != "resend" {
		return e.BadRequestError("provider must be \"smtp\" or \"resend\"", nil)
	}
	record, err := getOrCreateHubSettings(e.App)
	if err != nil {
		return err
	}
	record.Set("mail_provider", data.Provider)
	if data.ResendApiKey != "" {
		record.Set("resend_api_key", data.ResendApiKey)
	}
	if err := e.App.Save(record); err != nil {
		return err
	}
	return e.JSON(http.StatusOK, map[string]any{"success": true})
}
```

Registered in `internal/hub/api.go`, alongside the existing admin-only routes:

```go
// mail provider settings (SMTP / Resend)
apiAuth.GET("/mail-settings", alerts.GetMailSettings).BindFunc(requireAdminRole)
apiAuth.POST("/mail-settings", alerts.UpdateMailSettings).BindFunc(requireAdminRole)
```

Leaving `resendApiKey` blank in the `POST` body preserves the existing stored key — this lets an admin switch provider back and forth, or update just the provider, without needing to re-paste the key each time.

## 6. Frontend — Discord example note

`internal/site/src/components/routes/settings/notifications.tsx`, in the "Webhook / Push notifications" section, directly under the existing "Beszel uses Shoutrrr..." paragraph:

```tsx
<p className="text-sm text-muted-foreground leading-relaxed">
	<Trans>
		Example for Discord:{" "}
		<code className="bg-muted rounded-sm px-1 text-primary">discord://TOKEN@WEBHOOK_ID</code> — get{" "}
		<code className="bg-muted rounded-sm px-1 text-primary">TOKEN</code> and{" "}
		<code className="bg-muted rounded-sm px-1 text-primary">WEBHOOK_ID</code> from your Discord webhook URL (
		<code className="bg-muted rounded-sm px-1 text-primary">
			https://discord.com/api/webhooks/WEBHOOK_ID/TOKEN
		</code>
		).
	</Trans>
</p>
```

No backend change — the existing generic webhook field and `SendShoutrrrAlert`/`supportsTitle["discord"]` handling already work correctly with this URL shape.

## 7. Frontend — mail provider admin section

`internal/site/src/components/routes/settings/notifications.tsx`, the admin-only block (currently the "Please configure an SMTP server..." paragraph) is replaced by a new sub-component, e.g. `MailProviderSettings`, following the same fetch-on-mount pattern as `heartbeat.tsx`:

- `GET /api/beszel/mail-settings` on mount → `{ provider, providerSource, hasResendApiKey, resendApiKeySource }`.
- **If `providerSource === "env"` or `resendApiKeySource === "env"`**: render a read-only state (same shape as `heartbeat.tsx`'s `NotEnabledState`/`EnvVarItem`) showing which `BESZEL_HUB_*` variable(s) are controlling the setting and their current effective value (never the raw key — just confirmation a key is set). No editable inputs for the env-controlled part.
- **Otherwise (DB-backed, editable)**:
  - A `Select` with two options, `smtp` / `resend` (shadcn `Select`, matching existing form patterns in this codebase).
  - When `resend` is selected: an `Input type="password"` for the API key, with placeholder `"•••••••• (already set)"` when `hasResendApiKey` is `true`, left blank by default so submitting without touching it preserves the stored key (per section 5).
  - A dedicated "Save" button calling `POST /api/beszel/mail-settings` with `{ provider, resendApiKey }` — **separate** from the page's existing "Save Settings" button, since this hits a different, admin-only endpoint rather than the per-user `user_settings` collection.
- The existing link to `/_/#/settings/mail` ("configure an SMTP server") is kept, shown when the effective provider is `smtp`, since that's still where the real SMTP host/port/credentials are configured.

## 8. i18n

New `Trans`/`t` strings (Discord example note, provider select labels, API key input label/placeholder, save button, env-locked state text) are added in English (source locale) and extracted to all 30 locale catalogs with empty `msgstr`, hand-translated only into Spanish (`es.po`) — same workflow as the immediately preceding OOM Killer / MemAvailable / IO Pressure / Memory Pressure features (`npm run sync` inside `internal/site`, then edit `es/es.po`).

## 9. Testing

- `internal/alerts/mail_settings_test.go` (new): `resolveMailSettings` precedence — env wins over DB for provider and key independently; DB fallback when no env var; lazy creation of the singleton record on first call; `getOrCreateHubSettings` idempotency (second call returns the same record, doesn't duplicate).
- `internal/alerts/mail_resend_test.go` (new): `ResendMailer.Send` against an `httptest.Server` — asserts request method/headers/body shape, and that non-2xx responses surface as an error.
- `internal/alerts/alerts_api_test.go`: new cases for `GetMailSettings`/`UpdateMailSettings` — admin-role enforcement (non-admin gets 403 via `requireAdminRole`), round-trip of `provider`, and that a blank `resendApiKey` in the update body does not clear a previously stored key.
- `internal/alerts/alerts_test.go` or `alerts_system_test.go`: extend/verify `SendAlert`'s email path still works unchanged when `mail_provider` is `"smtp"` (default, no behavior change), and that it fails gracefully (logged error, no panic) when `resend` is selected without any API key configured.
- Manual: with the dev stack running, set `BESZEL_HUB_MAIL_PROVIDER=resend` + `BESZEL_HUB_RESEND_API_KEY=<test key>` and trigger a test alert to confirm an email actually arrives via Resend; separately, verify the Settings > Notifications admin UI reflects the env-locked state correctly, and that toggling the DB-backed setting (without the env vars set) persists across a page reload.

## Out of scope

- Any change to how Discord/Shoutrrr webhooks are sent — the existing `SendShoutrrrAlert` path is untouched.
- Per-user mail provider choice — Resend, like SMTP, is instance-wide only.
- Any other Shoutrrr-supported service getting a dedicated first-class UI field (Slack, Telegram, ntfy, etc.) — out of scope for this feature; only the Discord example-note request is addressed.
- Adding the official `resend-go` SDK as a dependency.
- Encrypting `resend_api_key` at rest — it's stored the same way PocketBase already stores the SMTP password (plaintext in the SQLite `_params`/collection tables), so this doesn't change the project's existing security posture for secrets-at-rest.
