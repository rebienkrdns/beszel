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
