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
