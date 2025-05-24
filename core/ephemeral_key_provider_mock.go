package core

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"golang.org/x/crypto/ssh"
	"github.com/juju/errors"
)

// EphemeralKeyProviderMock is a mock implementation for testing purposes.
// It generates a new RSA key pair each time GetEphemeralKey is called.
type EphemeralKeyProviderMock struct{}

// GetEphemeralKey generates a new RSA private key and returns it in PEM format.
func (m *EphemeralKeyProviderMock) GetEphemeralKey() ([]byte, error) {
	// Generate a new RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, errors.Annotatef(err, "generating RSA key for mock ephemeral provider")
	}

	// Convert to PEM format
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	return pem.EncodeToMemory(privateKeyPEM), nil
}

// GetAuthMethod returns an ssh.AuthMethod using the generated ephemeral key.
func (m *EphemeralKeyProviderMock) GetAuthMethod() (ssh.AuthMethod, error) {
	keyBytes, err := m.GetEphemeralKey()
	if err != nil {
		return nil, errors.Annotatef(err, "getting ephemeral key for auth method")
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, errors.Annotatef(err, "parsing ephemeral private key")
	}

	return ssh.PublicKeys(signer), nil
}
