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
		From:    formatAddress(message.From),
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

// formatAddress renders an address as "Name <email>" when a name is present,
// or as a bare email otherwise. mail.Address.String() always wraps the
// address in angle brackets, even without a name, which Resend's API
// doesn't require and can be avoided for a cleaner From header.
func formatAddress(a mail.Address) string {
	if a.Name == "" {
		return a.Address
	}
	return a.String()
}
