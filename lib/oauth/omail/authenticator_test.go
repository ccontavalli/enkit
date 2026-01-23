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

	"github.com/ccontavalli/enkit/lib/kemail"
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
			DialerFlags: kemail.DialerFlags{
				SmtpHost: "smtp.example.com",
				SmtpPort: 587,
			},
			TemplateFlags: kemail.TemplateFlags{
				BodyHTMLTemplate: []byte(kDefaultTemplateHTMLBody),
				BodyTextTemplate: []byte(kDefaultTemplateTextBody),
			},
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
	}

	callbackURL, err := url.Parse("https://example.com/auth/callback")
	assert.NoError(t, err)

	var sentMessage *gomail.Message
	mockDialer := &mockDialer{
		send: func(m *gomail.Message) error {
			sentMessage = m
			return nil
		},
	}

	auth, err := NewAuthenticator(
		rng,
		FromAuthenticatorFlags(authFlags),
		WithEmailerModifiers(WithCallbackURL(callbackURL), WithEmailerDialer(mockDialer)),
	)
	assert.NoError(t, err)

	// Test PerformLogin
	loginReq := httptest.NewRequest("POST", "/login?email=test@example.com", nil)
	loginRR := httptest.NewRecorder()

	err = auth.PerformLogin(loginRR, loginReq)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, loginRR.Code)
	bodyBytes, err := ioutil.ReadAll(loginRR.Body)
	assert.NoError(t, err)
	assert.Contains(t, string(bodyBytes), "Login email sent")
	assert.NotNil(t, sentMessage)

	// Extract token from email
	body := &bodyWriter{}
	_, err = sentMessage.WriteTo(body)
	assert.NoError(t, err)
	bodyStr := body.String()

	// Find the text part by its unique content
	marker := "open the following link in your browser"
	idx := strings.Index(bodyStr, marker)
	if !assert.True(t, idx > 0, "text part marker '%s' not found in email body.\nBody:\n%s", marker, bodyStr) {
		return
	}

	// Extract from marker until the next boundary (which starts with --)
	chunk := bodyStr[idx+len(marker):]
	endIdx := strings.Index(chunk, "--")
	if endIdx > 0 {
		chunk = chunk[:endIdx]
	}

	// Decode QP
	decoded, err := decodeQuotedPrintable(chunk)
	assert.NoError(t, err)

	// Now decoded should contain the URL: https://example.com/auth/callback?token=...
	tokenPrefix := "token="
	idx = strings.Index(decoded, tokenPrefix)
	assert.True(t, idx > 0, "token parameter not found in decoded text")

	tokenPart := decoded[idx+len(tokenPrefix):]
	// Token ends at the next whitespace (or end of string)
	tokenStr := strings.Fields(tokenPart)[0]
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
