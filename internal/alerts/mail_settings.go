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
