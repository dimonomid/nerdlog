package core

import (
	"os/exec"
	"strings"
	"golang.org/x/crypto/ssh"
	"github.com/juju/errors"
)

// EphemeralKeyProviderOpkssh implements EphemeralKeyProvider using opkssh.
type EphemeralKeyProviderOpkssh struct {
	OpksshPath string // Path to the opkssh binary
}

// GetEphemeralKey obtains an ephemeral private key from opkssh.
func (p *EphemeralKeyProviderOpkssh) GetEphemeralKey() ([]byte, error) {
	if p.OpksshPath == "" {
		p.OpksshPath = "opkssh"
	}

	cmd := exec.Command(p.OpksshPath, "key", "export", "--private")
	var out strings.Builder
	cmd.Stdout = &out
	
	err := cmd.Run()
	if err != nil {
		return nil, errors.Annotatef(err, "obtaining ephemeral key from opkssh")
	}

	return []byte(out.String()), nil
}

// GetAuthMethod returns an ssh.AuthMethod using the ephemeral key from opkssh.
func (p *EphemeralKeyProviderOpkssh) GetAuthMethod() (ssh.AuthMethod, error) {
	keyBytes, err := p.GetEphemeralKey()
	if err != nil {
		return nil, errors.Annotatef(err, "getting ephemeral key for auth method")
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, errors.Annotatef(err, "parsing ephemeral private key from opkssh")
	}

	return ssh.PublicKeys(signer), nil
}
