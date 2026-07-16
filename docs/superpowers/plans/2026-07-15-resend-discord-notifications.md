# Resend + Discord Notification Providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the Beszel hub send email alerts through Resend as an instance-wide alternative to SMTP (admin-selectable, env var or UI), and document the existing Discord webhook format in the Notifications UI.

**Architecture:** A new singleton `hub_settings` PocketBase collection (superuser-only rules) stores the chosen mail provider and Resend API key; a `resolveMailSettings` helper resolves the effective provider/key with `BESZEL_HUB_*` env vars taking precedence over the DB record. `SendAlert` picks between PocketBase's built-in SMTP mailer and a new `ResendMailer` (raw `net/http` POST to the Resend API) based on that resolution. Two admin-only routes (`GET`/`POST /api/beszel/mail-settings`) expose this to a new UI section in Settings > Notifications; the API key itself is never returned to the client. Discord gets a documentation-only UI change — the existing Shoutrrr webhook mechanism is untouched.

**Tech Stack:** Go (PocketBase framework), React + TypeScript (Vite), Lingui i18n, testify.

## Global Constraints

- Resend/SMTP selection is instance-wide, not per-user (same model as SMTP today).
- Environment variable wins over the DB-stored value, resolved independently for provider and API key: `BESZEL_HUB_MAIL_PROVIDER` and `BESZEL_HUB_RESEND_API_KEY` (read via the existing `utils.GetEnv` prefix convention).
- `hub_settings` collection: all five rules (`list`/`view`/`create`/`update`/`delete`) are `null` (superuser-only via generic REST). App-level `"admin"`-role users reach it only through the two new custom routes, gated by the existing `requireAdminRole` middleware.
- No new external Go dependency — the Resend client is implemented with `net/http`/`encoding/json` only.
- Discord requires no backend change — only a UI example note using the existing webhook field.
- i18n: new UI strings are added in English (source locale) via `Trans`/`t` macros, then extracted to all 30 locale catalogs with empty `msgstr`, hand-translated only into Spanish (`es.po`) — the same workflow used for the immediately preceding OOM Killer/MemAvailable/IO Pressure/Memory Pressure features.
- Test command for this repo: `go test -tags=testing ./...` (see `Makefile`'s `test` target). Frontend has no relevant automated test suite for this change; verify manually in the browser.

---

### Task 1: `hub_settings` collection + get-or-create helper

**Files:**
- Modify: `internal/migrations/0_collections_snapshot_0_19_0_dev_1.go:1698-1705`
- Create: `internal/alerts/mail_settings.go`
- Modify: `internal/alerts/alerts_test_helpers.go`
- Test: `internal/alerts/mail_settings_test.go` (new)

**Interfaces:**
- Produces: `getOrCreateHubSettings(app core.App) (*core.Record, error)` (unexported, package `alerts`) and its test wrapper `alerts.GetOrCreateHubSettings(app core.App) (*core.Record, error)`.
- Produces: the `hub_settings` collection, fields `mail_provider` (select: `"smtp"`/`"resend"`, default `"smtp"`) and `resend_api_key` (text).

- [ ] **Step 1: Write the failing test**

Create `internal/alerts/mail_settings_test.go`:

```go
//go:build testing

package alerts_test

import (
	"testing"

	"github.com/henrygd/beszel/internal/alerts"
	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetOrCreateHubSettings(t *testing.T) {
	hub, err := beszelTests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	defer hub.Cleanup()
	hub.StartHub()

	record, err := alerts.GetOrCreateHubSettings(hub)
	require.NoError(t, err)
	assert.Equal(t, "smtp", record.GetString("mail_provider"))
	assert.Equal(t, "", record.GetString("resend_api_key"))

	// second call must return the same record, not create a duplicate
	record2, err := alerts.GetOrCreateHubSettings(hub)
	require.NoError(t, err)
	assert.Equal(t, record.Id, record2.Id)

	count, err := hub.CountRecords("hub_settings")
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/alerts/... -run TestGetOrCreateHubSettings -v`
Expected: FAIL — `hub_settings` collection doesn't exist / `alerts.GetOrCreateHubSettings` undefined.

- [ ] **Step 3: Add the `hub_settings` collection to the migration snapshot**

In `internal/migrations/0_collections_snapshot_0_19_0_dev_1.go`, find this exact block (the end of the `universal_tokens` collection, immediately before the closing `]` of the JSON array):

```go
		"listRule": null,
		"name": "universal_tokens",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	}
]`
```

Replace it with:

```go
		"listRule": null,
		"name": "universal_tokens",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	},
	{
		"createRule": null,
		"deleteRule": null,
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
		"id": "pbc_4817263950",
		"indexes": [],
		"listRule": null,
		"name": "hub_settings",
		"system": false,
		"type": "base",
		"updateRule": null,
		"viewRule": null
	}
]`
```

- [ ] **Step 4: Create `internal/alerts/mail_settings.go`**

```go
// Package alerts handles alert management and delivery.
package alerts

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

const hubSettingsCollection = "hub_settings"

// getOrCreateHubSettings returns the singleton hub_settings record, creating
// it with defaults (mail_provider: "smtp") the first time it's requested.
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
```

- [ ] **Step 5: Add the test wrapper to `internal/alerts/alerts_test_helpers.go`**

Add this function anywhere in the file (it already has the `//go:build testing` tag and imports `core`):

```go
// GetOrCreateHubSettings returns (creating if necessary) the singleton hub_settings record.
func GetOrCreateHubSettings(app core.App) (*core.Record, error) {
	return getOrCreateHubSettings(app)
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test -tags=testing ./internal/alerts/... -run TestGetOrCreateHubSettings -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/migrations/0_collections_snapshot_0_19_0_dev_1.go internal/alerts/mail_settings.go internal/alerts/mail_settings_test.go internal/alerts/alerts_test_helpers.go
git commit -m "feat(hub): add hub_settings collection for instance-wide mail provider config"
```

---

### Task 2: `resolveMailSettings` (env-over-DB precedence)

**Files:**
- Modify: `internal/alerts/mail_settings.go`
- Modify: `internal/alerts/alerts_test_helpers.go`
- Test: `internal/alerts/mail_settings_test.go`

**Interfaces:**
- Consumes: `getOrCreateHubSettings(app core.App) (*core.Record, error)` from Task 1.
- Produces: exported type `alerts.MailSettingsInfo{ Provider, ProviderSource, HasResendApiKey bool, ResendApiKeySource string }` (JSON tags: `provider`, `providerSource`, `hasResendApiKey`, `resendApiKeySource`) and `resolveMailSettings(app core.App) (MailSettingsInfo, string, error)` (unexported; second return value is the resolved Resend API key, never part of `MailSettingsInfo`) plus its test wrapper `alerts.ResolveMailSettings`.

- [ ] **Step 1: Write the failing test**

Append to `internal/alerts/mail_settings_test.go`:

```go
func TestResolveMailSettings(t *testing.T) {
	hub, err := beszelTests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	defer hub.Cleanup()
	hub.StartHub()

	// default: no env vars, no DB overrides -> smtp / no key
	info, key, err := alerts.ResolveMailSettings(hub)
	require.NoError(t, err)
	assert.Equal(t, "smtp", info.Provider)
	assert.Equal(t, "db", info.ProviderSource)
	assert.False(t, info.HasResendApiKey)
	assert.Equal(t, "none", info.ResendApiKeySource)
	assert.Equal(t, "", key)

	// DB-backed override
	record, err := alerts.GetOrCreateHubSettings(hub)
	require.NoError(t, err)
	record.Set("mail_provider", "resend")
	record.Set("resend_api_key", "re_db_key")
	require.NoError(t, hub.Save(record))

	info, key, err = alerts.ResolveMailSettings(hub)
	require.NoError(t, err)
	assert.Equal(t, "resend", info.Provider)
	assert.Equal(t, "db", info.ProviderSource)
	assert.True(t, info.HasResendApiKey)
	assert.Equal(t, "db", info.ResendApiKeySource)
	assert.Equal(t, "re_db_key", key)

	// env vars win over DB, independently, for provider and key
	t.Setenv("BESZEL_HUB_MAIL_PROVIDER", "smtp")
	t.Setenv("BESZEL_HUB_RESEND_API_KEY", "re_env_key")

	info, key, err = alerts.ResolveMailSettings(hub)
	require.NoError(t, err)
	assert.Equal(t, "smtp", info.Provider)
	assert.Equal(t, "env", info.ProviderSource)
	assert.True(t, info.HasResendApiKey)
	assert.Equal(t, "env", info.ResendApiKeySource)
	assert.Equal(t, "re_env_key", key)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/alerts/... -run TestResolveMailSettings -v`
Expected: FAIL — `alerts.MailSettingsInfo`/`alerts.ResolveMailSettings` undefined.

- [ ] **Step 3: Implement `resolveMailSettings` in `internal/alerts/mail_settings.go`**

Add the import and the new type/function:

```go
import (
	"database/sql"
	"errors"

	"github.com/henrygd/beszel/internal/hub/utils"
	"github.com/pocketbase/pocketbase/core"
)
```

```go
// MailSettingsInfo is the client-facing view of the resolved mail configuration.
// The Resend API key itself is never included here - only whether one is
// configured and where it came from.
type MailSettingsInfo struct {
	Provider           string `json:"provider"`
	ProviderSource     string `json:"providerSource"`     // "env" | "db"
	HasResendApiKey    bool   `json:"hasResendApiKey"`
	ResendApiKeySource string `json:"resendApiKeySource"` // "env" | "db" | "none"
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

- [ ] **Step 4: Add the test wrapper to `internal/alerts/alerts_test_helpers.go`**

```go
// ResolveMailSettings resolves the effective mail provider and Resend API key.
func ResolveMailSettings(app core.App) (MailSettingsInfo, string, error) {
	return resolveMailSettings(app)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -tags=testing ./internal/alerts/... -run TestResolveMailSettings -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/alerts/mail_settings.go internal/alerts/mail_settings_test.go internal/alerts/alerts_test_helpers.go
git commit -m "feat(hub): resolve mail provider/API key with env-over-DB precedence"
```

---

### Task 3: `ResendMailer` (Resend HTTP client)

**Files:**
- Create: `internal/alerts/mail_resend.go`
- Test: `internal/alerts/mail_resend_test.go` (new)

**Interfaces:**
- Produces: exported `alerts.ResendMailer{ ApiKey string; ApiUrl string }` implementing `mailer.Mailer` (`Send(*mailer.Message) error`). `ApiUrl` overrides the Resend endpoint (used by tests); empty means the real `https://api.resend.com/emails`.

- [ ] **Step 1: Write the failing test**

Create `internal/alerts/mail_resend_test.go`:

```go
//go:build testing

package alerts_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"testing"

	"github.com/henrygd/beszel/internal/alerts"
	"github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResendMailerSend(t *testing.T) {
	var receivedAuth string
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := &alerts.ResendMailer{ApiKey: "re_test_key", ApiUrl: server.URL}
	err := m.Send(&mailer.Message{
		From:    mail.Address{Address: "alerts@example.com"},
		To:      []mail.Address{{Address: "user@example.com"}},
		Subject: "Test Alert",
		Text:    "Something happened.",
	})
	require.NoError(t, err)

	assert.Equal(t, "Bearer re_test_key", receivedAuth)
	assert.Equal(t, "alerts@example.com", receivedBody["from"])
	assert.Equal(t, "Test Alert", receivedBody["subject"])
	assert.Equal(t, []any{"user@example.com"}, receivedBody["to"])
}

func TestResendMailerSendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	m := &alerts.ResendMailer{ApiKey: "bad_key", ApiUrl: server.URL}
	err := m.Send(&mailer.Message{
		From:    mail.Address{Address: "alerts@example.com"},
		To:      []mail.Address{{Address: "user@example.com"}},
		Subject: "Test Alert",
		Text:    "Something happened.",
	})
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/alerts/... -run TestResendMailer -v`
Expected: FAIL — `alerts.ResendMailer` undefined.

- [ ] **Step 3: Implement `internal/alerts/mail_resend.go`**

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
	// ApiUrl overrides the Resend endpoint (used in tests). Empty uses the real Resend API.
	ApiUrl string
}

type resendEmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	Html    string   `json:"html,omitempty"`
}

func (r *ResendMailer) Send(message *mailer.Message) error {
	apiUrl := r.ApiUrl
	if apiUrl == "" {
		apiUrl = resendApiUrl
	}

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

	req, err := http.NewRequest(http.MethodPost, apiUrl, bytes.NewReader(body))
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

Note: `message.From.String()` (not just `.Address`) is used deliberately here since Resend's API accepts the RFC 5322 `"Name" <email>` form for `from` and displays the sender name when present; `addressList` for `to` uses bare addresses since Resend expects a plain array of email strings.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags=testing ./internal/alerts/... -run TestResendMailer -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/alerts/mail_resend.go internal/alerts/mail_resend_test.go
git commit -m "feat(hub): add ResendMailer implementing mailer.Mailer via Resend HTTP API"
```

---

### Task 4: Wire provider resolution into `SendAlert`

**Files:**
- Modify: `internal/alerts/alerts.go:249-254`
- Modify: `internal/alerts/alerts_test_helpers.go`
- Test: `internal/alerts/mail_settings_test.go`

**Interfaces:**
- Consumes: `resolveMailSettings` (Task 2), `ResendMailer` (Task 3).
- Produces: `(am *AlertManager) resolveMailClient() (mailer.Mailer, error)` (unexported) and test wrapper `(am *AlertManager) ResolveMailClient() (mailer.Mailer, error)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/alerts/mail_settings_test.go`:

```go
func TestResolveMailClient(t *testing.T) {
	hub, err := beszelTests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	defer hub.Cleanup()
	hub.StartHub()

	am := hub.GetAlertManager()

	// default: smtp provider -> not a ResendMailer
	client, err := am.ResolveMailClient()
	require.NoError(t, err)
	_, isResend := client.(*alerts.ResendMailer)
	assert.False(t, isResend)

	// switch to resend without a key configured -> should error
	record, err := alerts.GetOrCreateHubSettings(hub)
	require.NoError(t, err)
	record.Set("mail_provider", "resend")
	require.NoError(t, hub.Save(record))

	_, err = am.ResolveMailClient()
	assert.Error(t, err)

	// configure a key -> should now return a working ResendMailer
	record.Set("resend_api_key", "re_test_key")
	require.NoError(t, hub.Save(record))

	client, err = am.ResolveMailClient()
	require.NoError(t, err)
	resendClient, isResend := client.(*alerts.ResendMailer)
	require.True(t, isResend)
	assert.Equal(t, "re_test_key", resendClient.ApiKey)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/alerts/... -run TestResolveMailClient -v`
Expected: FAIL — `am.ResolveMailClient` undefined.

- [ ] **Step 3: Implement `resolveMailClient` and wire it into `SendAlert`**

In `internal/alerts/alerts.go`, replace:

```go
	err = am.hub.NewMailClient().Send(&message)
	if err != nil {
		return err
	}
```

with:

```go
	mailClient, err := am.resolveMailClient()
	if err != nil {
		am.hub.Logger().Error("Failed to resolve mail client", "err", err)
		return err
	}
	err = mailClient.Send(&message)
	if err != nil {
		return err
	}
```

Then add this new method directly after the `SendAlert` function (before the `SendShoutrrrAlert` function):

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

- [ ] **Step 4: Add the test wrapper to `internal/alerts/alerts_test_helpers.go`**

```go
// ResolveMailClient returns the mailer.Mailer that SendAlert would currently use.
func (am *AlertManager) ResolveMailClient() (mailer.Mailer, error) {
	return am.resolveMailClient()
}
```

This requires adding `"github.com/pocketbase/pocketbase/tools/mailer"` to the imports of `internal/alerts/alerts_test_helpers.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -tags=testing ./internal/alerts/... -run TestResolveMailClient -v`
Expected: PASS

- [ ] **Step 6: Run the full alerts package test suite to check for regressions**

Run: `go test -tags=testing ./internal/alerts/... -v`
Expected: PASS (all existing `SendAlert`/email-related tests still pass unchanged, since the default `"smtp"` path now goes through `resolveMailClient` but still ends up calling `am.hub.NewMailClient()` exactly as before).

- [ ] **Step 7: Commit**

```bash
git add internal/alerts/alerts.go internal/alerts/alerts_test_helpers.go internal/alerts/mail_settings_test.go
git commit -m "feat(hub): route SendAlert email delivery through resolved mail provider"
```

---

### Task 5: Admin API routes (`GET`/`POST /api/beszel/mail-settings`)

**Files:**
- Modify: `internal/alerts/alerts_api.go`
- Modify: `internal/hub/api.go:112-113`
- Modify: `internal/hub/api_test.go:624`

**Interfaces:**
- Consumes: `resolveMailSettings`, `getOrCreateHubSettings` (Tasks 1-2); `requireAdminRole` middleware (already exists in `internal/hub/api.go`).
- Produces: exported `alerts.GetMailSettings(e *core.RequestEvent) error` and `alerts.UpdateMailSettings(e *core.RequestEvent) error`, registered as `GET`/`POST /api/beszel/mail-settings`.

- [ ] **Step 1: Write the failing test**

In `internal/hub/api_test.go`, insert the following scenarios into the `scenarios` slice inside `TestApiRoutesAuthentication`, right after the `"POST /user-alerts - invalid auth token should fail"` scenario (i.e. immediately after its closing `},` at line 624, before the commented-out `GET /update` block):

```go
		{
			Name:            "GET /mail-settings - no auth should fail",
			Method:          http.MethodGet,
			URL:             "/api/beszel/mail-settings",
			ExpectedStatus:  401,
			ExpectedContent: []string{"requires valid"},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /mail-settings - with user auth should fail",
			Method: http.MethodGet,
			URL:    "/api/beszel/mail-settings",
			Headers: map[string]string{
				"Authorization": userToken,
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{"The authorized record is not allowed to perform this action."},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "GET /mail-settings - with admin auth should return default smtp provider",
			Method: http.MethodGet,
			URL:    "/api/beszel/mail-settings",
			Headers: map[string]string{
				"Authorization": adminUserToken,
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"provider":"smtp"`, `"hasResendApiKey":false`},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "POST /mail-settings - with user auth should fail",
			Method: http.MethodPost,
			URL:    "/api/beszel/mail-settings",
			Headers: map[string]string{
				"Authorization": userToken,
			},
			ExpectedStatus:  403,
			ExpectedContent: []string{"The authorized record is not allowed to perform this action."},
			TestAppFactory:  testAppFactory,
			Body: jsonReader(map[string]any{
				"provider": "resend",
			}),
		},
		{
			Name:   "POST /mail-settings - invalid provider should fail",
			Method: http.MethodPost,
			URL:    "/api/beszel/mail-settings",
			Headers: map[string]string{
				"Authorization": adminUserToken,
			},
			ExpectedStatus:  400,
			ExpectedContent: []string{"provider must be"},
			TestAppFactory:  testAppFactory,
			Body: jsonReader(map[string]any{
				"provider": "carrier-pigeon",
			}),
		},
		{
			Name:   "POST /mail-settings - with admin auth should switch to resend",
			Method: http.MethodPost,
			URL:    "/api/beszel/mail-settings",
			Headers: map[string]string{
				"Authorization": adminUserToken,
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"success":true`},
			TestAppFactory:  testAppFactory,
			Body: jsonReader(map[string]any{
				"provider":     "resend",
				"resendApiKey": "re_test_key_123",
			}),
		},
		{
			Name:   "GET /mail-settings - with admin auth should reflect resend provider",
			Method: http.MethodGet,
			URL:    "/api/beszel/mail-settings",
			Headers: map[string]string{
				"Authorization": adminUserToken,
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"provider":"resend"`, `"hasResendApiKey":true`, `"resendApiKeySource":"db"`},
			TestAppFactory:  testAppFactory,
		},
		{
			Name:   "POST /mail-settings - blank resendApiKey should not clear stored key",
			Method: http.MethodPost,
			URL:    "/api/beszel/mail-settings",
			Headers: map[string]string{
				"Authorization": adminUserToken,
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"success":true`},
			TestAppFactory:  testAppFactory,
			Body: jsonReader(map[string]any{
				"provider": "resend",
			}),
		},
		{
			Name:   "GET /mail-settings - key should still be set after blank-key update",
			Method: http.MethodGet,
			URL:    "/api/beszel/mail-settings",
			Headers: map[string]string{
				"Authorization": adminUserToken,
			},
			ExpectedStatus:  200,
			ExpectedContent: []string{`"hasResendApiKey":true`},
			TestAppFactory:  testAppFactory,
		},
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=testing ./internal/hub/... -run TestApiRoutesAuthentication -v`
Expected: FAIL — route `/api/beszel/mail-settings` doesn't exist (404) / `alerts.GetMailSettings` undefined (compile error).

- [ ] **Step 3: Implement the route handlers in `internal/alerts/alerts_api.go`**

Append these two functions after `DeleteUserAlerts` (no new imports needed — `net/http` and `core` are already imported in this file):

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

- [ ] **Step 4: Register the routes in `internal/hub/api.go`**

Find:

```go
	// send test notification
	apiAuth.POST("/test-notification", h.SendTestNotification)
```

Replace with:

```go
	// send test notification
	apiAuth.POST("/test-notification", h.SendTestNotification)
	// mail provider settings (SMTP / Resend)
	apiAuth.GET("/mail-settings", alerts.GetMailSettings).BindFunc(requireAdminRole)
	apiAuth.POST("/mail-settings", alerts.UpdateMailSettings).BindFunc(requireAdminRole)
```

(`alerts` is already imported in this file for `alerts.UpsertUserAlerts`/`alerts.DeleteUserAlerts`.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -tags=testing ./internal/hub/... -run TestApiRoutesAuthentication -v`
Expected: PASS

- [ ] **Step 6: Run the full backend test suite to check for regressions**

Run: `go test -tags=testing ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/alerts/alerts_api.go internal/hub/api.go internal/hub/api_test.go
git commit -m "feat(hub): add admin-only GET/POST /api/beszel/mail-settings routes"
```

---

### Task 6: Frontend — Discord example note

**Files:**
- Modify: `internal/site/src/components/routes/settings/notifications.tsx:124-139`

**Interfaces:**
- None (isolated UI text addition, no new props/state).

- [ ] **Step 1: Add the Discord example note**

In `internal/site/src/components/routes/settings/notifications.tsx`, find the "Webhook / Push notifications" header block:

```tsx
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
						</div>
```

Replace with:

```tsx
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
```

- [ ] **Step 2: Verify it renders**

Run: `cd internal/site && npm run dev` (or the project's existing dev-server command), open Settings > Notifications, and confirm the new example line appears under the Shoutrrr paragraph with the three `code`-styled placeholders.

- [ ] **Step 3: Commit**

```bash
git add internal/site/src/components/routes/settings/notifications.tsx
git commit -m "feat(ui): add Discord webhook URL example to notifications settings"
```

---

### Task 7: Frontend — mail provider admin section

**Files:**
- Modify: `internal/site/src/components/routes/settings/notifications.tsx`

**Interfaces:**
- Consumes: `GET`/`POST /api/beszel/mail-settings` (Task 5), response shape `{ provider, providerSource, hasResendApiKey, resendApiKeySource }`.

- [ ] **Step 1: Replace the admin-only SMTP hint with a `MailProviderSettings` component**

In `internal/site/src/components/routes/settings/notifications.tsx`, find:

```tsx
						{isAdmin() && (
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
```

Replace with:

```tsx
						{isAdmin() && <MailProviderSettings />}
```

- [ ] **Step 2: Add the `MailProviderSettings` component**

Add this near the bottom of the file, right above `export default SettingsNotificationsPage`:

```tsx
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
```

- [ ] **Step 3: Add the new imports**

At the top of the file, add `Select`/`SelectContent`/`SelectItem`/`SelectTrigger`/`SelectValue` to the existing import block:

```tsx
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
```

- [ ] **Step 4: Verify manually in the browser**

Run: `cd internal/site && npm run dev` (or existing dev command), log in as an admin user, open Settings > Notifications:
- Confirm the section shows "SMTP" selected by default and the existing "configure an SMTP server" link.
- Switch to "Resend", type a fake key, click "Save mail provider" — confirm the toast says saved, and reloading the page shows the placeholder `"•••••••• (already set)"`.
- With `BESZEL_HUB_MAIL_PROVIDER=resend` set in the hub's environment (restart the dev hub with it set), confirm the section instead shows the read-only env-locked state with no editable controls.

- [ ] **Step 5: Commit**

```bash
git add internal/site/src/components/routes/settings/notifications.tsx
git commit -m "feat(ui): add admin mail provider (SMTP/Resend) settings section"
```

---

### Task 8: i18n extraction + Spanish translation

**Files:**
- Modify: all `internal/site/src/locales/*/*.po` (generated)
- Modify: `internal/site/src/locales/es/es.po` (hand-translated)

**Interfaces:**
- None (build/translation step only).

- [ ] **Step 1: Extract new strings**

Run:
```bash
cd internal/site && npm run sync
```
Expected: new `msgid`s appear (with empty `msgstr`) in all 30 `internal/site/src/locales/*/*.po` files for the new strings introduced in Tasks 6 and 7 (the Discord example note, "Mail provider", "Mail provider is controlled by environment variables on this hub", "Active provider:", "Resend API key", "Leave blank to keep the currently saved key.", "Save mail provider", "Mail provider settings saved", "Failed to save mail provider settings", "Enter Resend API key...").

- [ ] **Step 2: Hand-translate the new strings into Spanish**

Open `internal/site/src/locales/es/es.po`, find each new empty `msgstr ""` whose `msgid` matches the English strings from Step 1, and fill in the Spanish translation, e.g.:

```po
msgid "Mail provider"
msgstr "Proveedor de correo"

msgid "Enter Resend API key..."
msgstr "Ingresa la API key de Resend..."

msgid "Leave blank to keep the currently saved key."
msgstr "Déjalo en blanco para conservar la clave guardada."

msgid "Save mail provider"
msgstr "Guardar proveedor de correo"
```

(Translate every new string introduced by Tasks 6-7, following the exact `msgid` text generated in Step 1 — the `.po` tool auto-generates the `msgid`s, so copy them verbatim rather than retyping from this plan.)

- [ ] **Step 3: Compile catalogs**

Run:
```bash
cd internal/site && npm run sync
```
(This both re-extracts and compiles; running it again after editing `es.po` regenerates the compiled catalogs used at runtime without clobbering the hand-added Spanish translations, since `lingui extract --overwrite` only touches `msgid`s/comments, not existing `msgstr` values.)

- [ ] **Step 4: Verify in the browser**

Switch the UI language to Spanish (existing language switcher) and confirm the new mail-provider section and Discord note render in Spanish.

- [ ] **Step 5: Commit**

```bash
git add internal/site/src/locales
git commit -m "i18n: extract Resend/Discord notification strings, add Spanish translations"
```

---

### Task 9: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full backend build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 2: Full backend test suite**

Run: `go test -tags=testing ./...`
Expected: all tests PASS, including every new test added in Tasks 1-5.

- [ ] **Step 3: Frontend build**

Run: `cd internal/site && npm run build`
Expected: build succeeds (this also runs `lingui extract --overwrite && lingui compile` per the `build` script, so it doubles as a check that Task 8's i18n additions compile cleanly).

- [ ] **Step 4: Manual end-to-end check**

With the dev stack running (hub + at least one agent):
1. Leave `mail_provider` at its default (`smtp`) — trigger a test alert and confirm email delivery is unchanged from before this feature.
2. In Settings > Notifications (as admin), switch to Resend, enter a real Resend API key and a verified sending domain's address as the hub's sender address (`/_/#/settings/mail` "Sender address"), save, then trigger a test alert and confirm the email actually arrives via Resend.
3. Set `BESZEL_HUB_MAIL_PROVIDER=resend` and `BESZEL_HUB_RESEND_API_KEY=<key>` as environment variables on the hub, restart it, and confirm the admin UI now shows the read-only env-locked state.
4. Paste a real `discord://TOKEN@WEBHOOK_ID` URL into the existing webhook field and use the "Test URL" button to confirm the message arrives in Discord, cross-checking against the new example note's format.

- [ ] **Step 5: Commit (only if verification uncovered fixes)**

If Step 4 surfaces any bug fixes, commit them separately with a descriptive message; otherwise this task requires no commit of its own.
