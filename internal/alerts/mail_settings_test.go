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
