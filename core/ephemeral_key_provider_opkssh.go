package core

import (
	"bytes"
	"errors"
	"os/exec"

	"golang.org/x/crypto/ssh"
)

// OpksshEphemeralKeyProvider is a prototype implementation of EphemeralKeyProvider
// that interacts with the opkssh CLI tool to obtain ephemeral SSH keys.
type OpksshEphemeralKeyProvider struct {
	// Path to the opkssh CLI executable
	OpksshPath string
}

// GetEphemeralKey runs the opkssh CLI to obtain the ephemeral private key.
func (p *OpksshEphemeralKeyProvider) GetEphemeralKey() ([]byte, error) {
	if p.OpksshPath == "" {
		p.OpksshPath = "opkssh"
	}

	cmd := exec.Command(p.OpksshPath, "key", "export", "--private")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, errors.New("failed to obtain ephemeral key from opkssh: " + err.Error())
	}

	return out.Bytes(), nil
}

// GetAuthMethod returns an ssh.AuthMethod using the ephemeral key obtained from opkssh.
func (p *OpksshEphemeralKeyProvider) GetAuthMethod() (interface{}, error) {
	keyData, err := p.GetEphemeralKey()
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, errors.New("failed to parse ephemeral private key: " + err.Error())
	}

	return ssh.PublicKeys(signer), nil
}
