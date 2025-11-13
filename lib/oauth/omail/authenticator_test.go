package omail

import (
	"io/ioutil"
	"math/rand"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/lib/srand"
	"github.com/ccontavalli/enkit/lib/token"
	"github.com/stretchr/testify/assert"
	"gopkg.in/gomail.v2"
)

func decodeQuotedPrintable(s string) (string, error) {
	r := quotedprintable.NewReader(strings.NewReader(s))
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestAuthenticator(t *testing.T) {
	rng := rand.New(srand.Source)
	key, err := token.GenerateSymmetricKey(rng, 256)
	assert.NoError(t, err)
	verify, sign, err := token.GenerateSigningKey(rng)
	assert.NoError(t, err)

	authFlags := &AuthenticatorFlags{
		EmailerFlags: EmailerFlags{
			SmtpHost:      "smtp.example.com",
			SmtpPort:      587,
			FromAddress:   "noreply@example.com",
			TokenLifetime: 15 * time.Minute,
			SymmetricKey:  key,
		},
		SigningExtractorFlags: oauth.SigningExtractorFlags{
			ExtractorFlags: &oauth.ExtractorFlags{
				LoginTime:         24 * time.Hour,
				SymmetricKey:      key,
				TokenVerifyingKey: (*verify.ToBytes())[:],
			},
			TokenSigningKey: (*sign.ToBytes())[:],
		},
		EmailSentRedirectURL: "https://example.com/email-sent",
	}

	callbackURL, err := url.Parse("https://example.com/auth/callback")
	assert.NoError(t, err)

	auth, err := NewAuthenticator(
		rng,
		FromAuthenticatorFlags(authFlags),
		WithEmailerModifiers(WithCallbackURL(callbackURL)),
	)
	assert.NoError(t, err)

	var sentMessage *gomail.Message
	auth.Emailer.dialer = &mockDialer{
		send: func(m *gomail.Message) error {
			sentMessage = m
			return nil
		},
	}

	// Test PerformLogin
	loginReq := httptest.NewRequest("POST", "/login?email=test@example.com", nil)
	loginRR := httptest.NewRecorder()

	err = auth.PerformLogin(loginRR, loginReq)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusFound, loginRR.Code)
	assert.Equal(t, "https://example.com/email-sent", loginRR.Header().Get("Location"))
	assert.NotNil(t, sentMessage)

	// Extract token from email
	body := &bodyWriter{}
	_, err = sentMessage.WriteTo(body)
	assert.NoError(t, err)
	bodyStr, err := decodeQuotedPrintable(body.String())
	assert.NoError(t, err)

	urlIndex := strings.Index(bodyStr, "https://example.com/auth/callback?token=")
	assert.True(t, urlIndex > 0)
	tokenStr := strings.TrimSpace(bodyStr[urlIndex+len("https://example.com/auth/callback?token="):])
	t.Logf("Extracted token: %s", tokenStr)

	// Test PerformAuth
	authReq := httptest.NewRequest("GET", "/auth/callback?token="+tokenStr, nil)
	authRR := httptest.NewRecorder()

	authData, err := auth.PerformAuth(authRR, authReq)
	assert.NoError(t, err)
	assert.NotNil(t, authData)
	assert.Equal(t, "test", authData.Creds.Identity.Username)

	// Verify the cookie
	cookie := authRR.Result().Cookies()[0]
	assert.NotEmpty(t, cookie.Value)

	// Test GetCredentialsFromRequest
	credReq := httptest.NewRequest("GET", "/", nil)
	credReq.AddCookie(cookie)

	creds, _, err := auth.GetCredentialsFromRequest(credReq)
	assert.NoError(t, err)
	assert.NotNil(t, creds)
	assert.Equal(t, "test", creds.Identity.Username)
}