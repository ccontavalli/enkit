package omail

import (
	"math/rand"
	"net/url"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/kemail"
	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/lib/srand"
	"github.com/ccontavalli/enkit/lib/token"
	"github.com/stretchr/testify/assert"
	"gopkg.in/gomail.v2"
)

type mockDialer struct {
	send func(m *gomail.Message) error
}

func (d *mockDialer) DialAndSend(m ...*gomail.Message) error {
	if d.send != nil {
		return d.send(m[0])
	}
	return nil
}

func TestEmailer(t *testing.T) {
	var sentMessage *gomail.Message
	mockDialer := &mockDialer{
		send: func(m *gomail.Message) error {
			sentMessage = m
			return nil
		},
	}

	rng := rand.New(srand.Source)
	key, err := token.GenerateSymmetricKey(rng, 256)
	assert.NoError(t, err)

	flags := &EmailerFlags{
		DialerFlags: kemail.DialerFlags{
			SmtpHost: "smtp.example.com",
			SmtpPort: 587,
		},
		TemplateFlags: kemail.TemplateFlags{
			SubjectTemplate:  []byte("Welcome {{.name}}!"),
			BodyHTMLTemplate: []byte("HTML Token for {{.email}}: {{.URL}} Key: {{.custom_key}}"),
			BodyTextTemplate: []byte("Text Token for {{.email}}: {{.URL}} Key: {{.custom_key}}"),
		},
		FromAddress:   "noreply@example.com",
		SymmetricKey:  key,
		TokenLifetime: 15 * time.Minute,
	}

	callbackURL, err := url.Parse("https://example.com/my/callback")
	assert.NoError(t, err)

	emailer, err := NewEmailer(rng, FromEmailerFlags(flags), WithCallbackURL(callbackURL), WithEmailerDialer(mockDialer))
	assert.NoError(t, err)

	// Test CreateEmailToken and ValidateEmailToken
	params := url.Values{}
	params.Set("email", "test@example.com")
	params.Set("name", "Test User")

	tokenStr, err := emailer.CreateEmailToken(params, oauth.WithTarget("/return-here"), oauth.WithState("my-state"))
	assert.NoError(t, err)
	assert.NotEmpty(t, tokenStr)

	payload, err := emailer.ValidateEmailToken(tokenStr)
	assert.NoError(t, err)
	assert.NotNil(t, payload)
	assert.Equal(t, "email:test@example.com", payload.Creds.Identity.Id)
	assert.Equal(t, "/return-here", payload.Target)
	assert.Equal(t, "my-state", payload.State)

	// Test SendLoginEmail with extra data
	extraData := map[string]interface{}{
		"custom_key": "custom_value",
	}
	err = emailer.SendLoginEmail(params, "test-location", oauth.WithTarget("/return-here"), oauth.WithState("my-state"), oauth.WithTemplateData(extraData))
	assert.NoError(t, err)

	assert.NotNil(t, sentMessage)
	assert.Equal(t, "test@example.com", sentMessage.GetHeader("To")[0])
	assert.Equal(t, "Welcome Test User!", sentMessage.GetHeader("Subject")[0])

	body := &bodyWriter{}
	_, err = sentMessage.WriteTo(body)
	assert.NoError(t, err)

	bodyStr := body.String()
	// Check HTML part
	assert.Contains(t, bodyStr, "HTML Token for test@example.com:")
	assert.Contains(t, bodyStr, `Key: custom_value`)
	// Check Text part
	assert.Contains(t, bodyStr, "Text Token for test@example.com:")
	assert.Contains(t, bodyStr, `https://example.com/my/callback?token=`)

	// Check Content-Type is multipart/alternative
	assert.Contains(t, bodyStr, "Content-Type: multipart/alternative")
}

func TestFlagsValidation(t *testing.T) {
	rng := rand.New(srand.Source)
	callbackURL, err := url.Parse("/my/callback")
	assert.NoError(t, err)

	validBody := []byte("{{.URL}}")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpPort: 587}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: validBody, BodyTextTemplate: validBody}}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "smtp-host")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpHost: "smtp.example.com", SmtpPort: 587}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: validBody, BodyTextTemplate: validBody}}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "from-address")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpHost: "smtp.example.com", SmtpPort: 0}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: validBody, BodyTextTemplate: validBody}, FromAddress: "test@test.com"}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "smtp-port")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpHost: "smtp.example.com", SmtpPort: 70000}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: validBody, BodyTextTemplate: validBody}, FromAddress: "test@test.com"}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "smtp-port")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpHost: "smtp.example.com", SmtpPort: 587}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: []byte("{{.URL}} {{.Invalid")}, FromAddress: "test@test.com"}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed action")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpHost: "smtp.example.com", SmtpPort: 587}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: validBody, BodyTextTemplate: []byte("{{.URL}} {{.Invalid")}, FromAddress: "test@test.com"}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed action")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpHost: "smtp.example.com", SmtpPort: 587}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: []byte("no url")}, FromAddress: "test@test.com"}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "body html template must contain {{.URL}}")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpHost: "smtp.example.com", SmtpPort: 587}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: validBody, BodyTextTemplate: []byte("no url")}, FromAddress: "test@test.com"}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "body text template must contain {{.URL}}")

	// Test that a key is generated if not provided
	emailer, err := NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpHost: "smtp.example.com", SmtpPort: 587}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: validBody, BodyTextTemplate: validBody}, FromAddress: "test@test.com"}), WithCallbackURL(callbackURL))
	assert.NoError(t, err)
	assert.NotNil(t, emailer)

	// Test that callback URL is required
	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{DialerFlags: kemail.DialerFlags{SmtpHost: "smtp.example.com", SmtpPort: 587}, TemplateFlags: kemail.TemplateFlags{BodyHTMLTemplate: validBody, BodyTextTemplate: validBody}, FromAddress: "test@test.com"}))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "CallbackURL must be configured")
}
