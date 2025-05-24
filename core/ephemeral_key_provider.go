package core

import (
	"golang.org/x/crypto/ssh"
	"github.com/juju/errors"
)

// EphemeralKeyProvider defines an interface for obtaining ephemeral SSH keys and authentication methods.
type EphemeralKeyProvider interface {
	// GetEphemeralKey returns the private key bytes.
	GetEphemeralKey() ([]byte, error)

	// GetAuthMethod returns an ssh.AuthMethod that uses the ephemeral key for authentication.
	GetAuthMethod() (ssh.AuthMethod, error)
}

// ErrEphemeralKeyNotAvailable is returned when ephemeral keys are not available or the provider is not configured.
var ErrEphemeralKeyNotAvailable = errors.New("ephemeral key not available")

// DummyEphemeralKeyProvider is a stub implementation that always returns ErrEphemeralKeyNotAvailable.
type DummyEphemeralKeyProvider struct{}

func (d *DummyEphemeralKeyProvider) GetEphemeralKey() ([]byte, error) {
	return nil, ErrEphemeralKeyNotAvailable
}

func (d *DummyEphemeralKeyProvider) GetAuthMethod() (ssh.AuthMethod, error) {
	return nil, ErrEphemeralKeyNotAvailable
}
