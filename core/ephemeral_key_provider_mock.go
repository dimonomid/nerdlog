package core

import (
	"errors"

	"golang.org/x/crypto/ssh"
)

// MockEphemeralKeyProvider is a mock implementation of EphemeralKeyProvider for testing.
type MockEphemeralKeyProvider struct {
	KeyData []byte
	Signer  ssh.Signer
	Err     error
}

func (m *MockEphemeralKeyProvider) GetEphemeralKey() ([]byte, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.KeyData, nil
}

func (m *MockEphemeralKeyProvider) GetAuthMethod() (interface{}, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Signer == nil {
		return nil, errors.New("no signer available")
	}
	return ssh.PublicKeys(m.Signer), nil
}
