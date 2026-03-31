package omail

import (
	"io"
	"math/rand"
	"mime"
	"mime/multipart"
	"net/mail"
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

func extractTokenFromMessage(m *gomail.Message) (string, error) {
	body := &bodyWriter{}
	if _, err := m.WriteTo(body); err != nil {
		return "", err
	}

	msg, err := mail.ReadMessage(strings.NewReader(body.String()))
	if err != nil {
		return "", err
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}

	var textBody string
	if strings.HasPrefix(mediaType, "multipart/") {
		reader := multipart.NewReader(msg.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}

			if !strings.HasPrefix(part.Header.Get("Content-Type"), "text/plain") {
				continue
			}

			data, err := io.ReadAll(part)
			if err != nil {
				return "", err
			}
			textBody = string(data)
			break
		}
	} else {
		data, err := io.ReadAll(msg.Body)
		if err != nil {
			return "", err
		}
		textBody = string(data)
	}

	for _, field := range strings.Fields(textBody) {
		if !strings.HasPrefix(field, "https://") {
			continue
		}
		tokenURL, err := url.Parse(field)
		if err != nil {
			return "", err
		}
		return tokenURL.Query().Get("token"), nil
	}
	return "", io.EOF
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
	bodyBytes, err := io.ReadAll(loginRR.Body)
	assert.NoError(t, err)
	assert.Contains(t, string(bodyBytes), "Login email sent")
	assert.NotNil(t, sentMessage)

	tokenStr, err := extractTokenFromMessage(sentMessage)
	if !assert.NoError(t, err) {
		return
	}
	if !assert.NotEmpty(t, tokenStr) {
		return
	}

	// Test PerformAuth
	authReq := httptest.NewRequest("GET", "/auth/callback?token="+tokenStr, nil)
	authRR := httptest.NewRecorder()

	authData, err := auth.PerformAuth(authRR, authReq)
	if !assert.NoError(t, err) {
		return
	}
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
