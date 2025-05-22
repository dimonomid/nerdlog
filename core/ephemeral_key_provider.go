package core

import (
	"errors"
)

// EphemeralKeyProvider defines an interface for obtaining ephemeral SSH keys and related authentication data.
type EphemeralKeyProvider interface {
	// GetEphemeralKey returns the private key bytes and any associated metadata or error.
	GetEphemeralKey() ([]byte, error)

	// GetAuthMethod returns an ssh.AuthMethod that uses the ephemeral key for authentication.
	GetAuthMethod() (interface{}, error)
}

// ErrEphemeralKeyNotAvailable is returned when ephemeral keys are not available or the provider is not configured.
var ErrEphemeralKeyNotAvailable = errors.New("ephemeral key not available")

// DummyEphemeralKeyProvider is a stub implementation that always returns ErrEphemeralKeyNotAvailable.
type DummyEphemeralKeyProvider struct{}

func (d *DummyEphemeralKeyProvider) GetEphemeralKey() ([]byte, error) {
	return nil, ErrEphemeralKeyNotAvailable
}

func (d *DummyEphemeralKeyProvider) GetAuthMethod() (interface{}, error) {
	return nil, ErrEphemeralKeyNotAvailable
}
