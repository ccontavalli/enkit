package kemail

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/gomail.v2"
)

type fakeSendDialer struct {
	send func(m *gomail.Message) error
}

func (d *fakeSendDialer) DialAndSend(m ...*gomail.Message) error {
	if d.send == nil {
		return nil
	}
	return d.send(m[0])
}

func TestParseTemplatesValidation(t *testing.T) {
	_, err := ParseTemplates(nil, []byte("html"), []byte("text"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "subject template is required")

	_, err = ParseTemplates([]byte("subject"), nil, []byte("text"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "body html template is required")

	_, err = ParseTemplates([]byte("subject"), []byte("html"), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "body text template is required")
}

func TestTransactionalEmailerSend(t *testing.T) {
	templates, err := ParseTemplates(
		[]byte("Welcome {{.name}}"),
		[]byte("<p>Hello {{.name}}</p>"),
		[]byte("Hello {{.name}}"),
	)
	assert.NoError(t, err)

	var sentMessage *gomail.Message
	dialer := &fakeSendDialer{
		send: func(m *gomail.Message) error {
			sentMessage = m
			return nil
		},
	}

	emailer, err := NewTransactionalEmailer(
		WithDialer(dialer),
		WithFromAddress("noreply@example.com"),
		WithTemplates(templates),
	)
	assert.NoError(t, err)

	err = emailer.Send("user@example.com", map[string]interface{}{"name": "Test User"})
	assert.NoError(t, err)
	assert.NotNil(t, sentMessage)
	assert.Equal(t, "noreply@example.com", sentMessage.GetHeader("From")[0])
	assert.Equal(t, "user@example.com", sentMessage.GetHeader("To")[0])
	assert.Equal(t, "Welcome Test User", sentMessage.GetHeader("Subject")[0])

	var body bytes.Buffer
	_, err = sentMessage.WriteTo(&body)
	assert.NoError(t, err)
	bodyStr := body.String()
	assert.Contains(t, bodyStr, "Hello Test User")
	assert.Contains(t, bodyStr, "Content-Type: multipart/alternative")
}

func TestTransactionalEmailerSendError(t *testing.T) {
	templates, err := ParseTemplates(
		[]byte("Subject"),
		[]byte("<p>HTML</p>"),
		[]byte("Text"),
	)
	assert.NoError(t, err)

	sendErr := errors.New("send failed")
	emailer, err := NewTransactionalEmailer(
		WithDialer(&fakeSendDialer{send: func(m *gomail.Message) error {
			return sendErr
		}}),
		WithFromAddress("noreply@example.com"),
		WithTemplates(templates),
	)
	assert.NoError(t, err)

	err = emailer.Send("user@example.com", map[string]interface{}{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error sending email")
}
