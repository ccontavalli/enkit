package oauth

import (
	"github.com/ccontavalli/enkit/lib/logger"
	"golang.org/x/oauth2"
)

// Verifier is an object capable of verifying an oauth2.Token after obtaining it.
//
// Verifiers can also add information retrieved from the remote provider to the
// identity, using some provider specific mechanisms.
//
// For example, they can check if a domain matches a list of allowed domains, or
// retrieve a list of groups and add them as part of the user identity.
type Verifier interface {
	Scopes() []string
	Verify(log logger.Logger, identity *Identity, tok *oauth2.Token) (*Identity, error)
}

type VerifierFactory func(conf *oauth2.Config) (Verifier, error)

type OptionalVerifier struct {
	inner Verifier
}

func (ov *OptionalVerifier) Scopes() []string {
	return ov.inner.Scopes()
}

func (ov *OptionalVerifier) Verify(log logger.Logger, identity *Identity, tok *oauth2.Token) (*Identity, error) {
	result, err := ov.inner.Verify(log, identity, tok)
	if err != nil {
		user := "<unknown>"
		if identity != nil {
			user = identity.GlobalName()
		}

		log.Errorf("for user %s - ignored verifier %T - error: %s", user, ov.inner, err)
		return identity, nil
	}
	return result, nil
}

func NewOptionalVerifierFactory(factory VerifierFactory) VerifierFactory {
	return func(conf *oauth2.Config) (Verifier, error) {
		inner, err := factory(conf)
		if err != nil {
			return nil, err
		}
		return &OptionalVerifier{inner: inner}, nil
	}
}
