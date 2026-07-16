package alerts

import (
	"database/sql"
	"errors"

	"github.com/henrygd/beszel/internal/hub/utils"
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
