package core

import (
	"errors"
	"io"
	"testing"

	"golang.org/x/crypto/ssh"
)

type dummySigner struct{}

func (d *dummySigner) PublicKey() ssh.PublicKey {
	return nil
}

func (d *dummySigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	return nil, nil
}

type failingEphemeralKeyProvider struct{}

func (f *failingEphemeralKeyProvider) GetEphemeralKey() ([]byte, error) {
	return nil, errors.New("failed to get ephemeral key")
}

func (f *failingEphemeralKeyProvider) GetAuthMethod() (interface{}, error) {
	return nil, errors.New("failed to get auth method")
}

func TestGetSSHAuthMethodWithEphemeralKey(t *testing.T) {
	mockProvider := &MockEphemeralKeyProvider{
		Signer: &dummySigner{},
	}

	st := &ShellTransportSSH{
		params: ShellTransportSSHParams{
			EphemeralKeyProvider: mockProvider,
			SSHKeys:              []string{},
		},
	}

	authMethod, err := st.getSSHAuthMethod(nil, nil)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if authMethod == nil {
		t.Fatalf("Expected auth method, got nil")
	}
}

func TestGetSSHAuthMethodFallback(t *testing.T) {
	mockProvider := &MockEphemeralKeyProvider{
		Err: ErrEphemeralKeyNotAvailable,
	}

	st := &ShellTransportSSH{
		params: ShellTransportSSHParams{
			EphemeralKeyProvider: mockProvider,
			SSHKeys:              []string{"nonexistent_key"},
		},
	}

	_, err := st.getSSHAuthMethod(nil, nil)
	if err == nil {
		t.Fatalf("Expected error due to missing keys, got nil")
	}
}

func TestGetSSHAuthMethodEphemeralFailureFallback(t *testing.T) {
	failProvider := &failingEphemeralKeyProvider{}

	st := &ShellTransportSSH{
		params: ShellTransportSSHParams{
			EphemeralKeyProvider: failProvider,
			SSHKeys:              []string{"nonexistent_key"},
		},
	}

	_, err := st.getSSHAuthMethod(nil, nil)
	if err == nil {
		t.Fatalf("Expected error due to ephemeral key failure and missing keys, got nil")
	}
}
