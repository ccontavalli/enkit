package omail

import (
	"math/rand"
	"net/url"
	"testing"
	"time"

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
		SmtpHost:        "smtp.example.com",
		SmtpPort:        587,
		FromAddress:     "noreply@example.com",
		SymmetricKey:    key,
		TokenLifetime:   15 * time.Minute,
		SubjectTemplate: []byte("Welcome {{.name}}!"),
		BodyTemplate:    []byte("Token for {{.email}}: {{.URL}}"),
	}

	callbackURL, err := url.Parse("https://example.com/my/callback")
	assert.NoError(t, err)

	emailer, err := NewEmailer(rng, FromEmailerFlags(flags), WithCallbackURL(callbackURL))
	assert.NoError(t, err)
	emailer.dialer = mockDialer

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
	assert.Equal(t, "test@example.com", payload.Email)
	assert.Equal(t, "/return-here", payload.Target)
	assert.Equal(t, "my-state", payload.State)

	// Test SendLoginEmail
	err = emailer.SendLoginEmail(params, oauth.WithTarget("/return-here"), oauth.WithState("my-state"))
	assert.NoError(t, err)

	assert.NotNil(t, sentMessage)
	assert.Equal(t, "test@example.com", sentMessage.GetHeader("To")[0])
	assert.Equal(t, "Welcome Test User!", sentMessage.GetHeader("Subject")[0])

	body := &bodyWriter{}
	_, err = sentMessage.WriteTo(body)
	assert.NoError(t, err)

	bodyStr := body.String()
	assert.Contains(t, bodyStr, "Token for test@example.com:")
	assert.Contains(t, bodyStr, `https://example.com/my/callback?token=`)
}

func TestFlagsValidation(t *testing.T) {
	rng := rand.New(srand.Source)
	callbackURL, err := url.Parse("/my/callback")
	assert.NoError(t, err)

	validBody := []byte("{{.URL}}")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{SmtpPort: 587, BodyTemplate: validBody}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "smtp-host")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{SmtpHost: "smtp.example.com", SmtpPort: 587, BodyTemplate: validBody}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "from-address")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{SmtpHost: "smtp.example.com", FromAddress: "test@test.com", SmtpPort: 0, BodyTemplate: validBody}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "smtp-port")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{SmtpHost: "smtp.example.com", FromAddress: "test@test.com", SmtpPort: 70000, BodyTemplate: validBody}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "smtp-port")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{SmtpHost: "smtp.example.com", FromAddress: "test@test.com", SmtpPort: 587, BodyTemplate: []byte("{{.URL}} {{.Invalid")}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed action")

	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{SmtpHost: "smtp.example.com", FromAddress: "test@test.com", SmtpPort: 587, BodyTemplate: []byte("no url")}), WithCallbackURL(callbackURL))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "body template must contain {{.URL}}")

	// Test that a key is generated if not provided
	emailer, err := NewEmailer(rng, FromEmailerFlags(&EmailerFlags{SmtpHost: "smtp.example.com", FromAddress: "test@test.com", SmtpPort: 587, BodyTemplate: validBody}), WithCallbackURL(callbackURL))
	assert.NoError(t, err)
	assert.NotNil(t, emailer)

	// Test that callback URL is required
	_, err = NewEmailer(rng, FromEmailerFlags(&EmailerFlags{SmtpHost: "smtp.example.com", FromAddress: "test@test.com", SmtpPort: 587, BodyTemplate: validBody}))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "CallbackURL must be configured")
}