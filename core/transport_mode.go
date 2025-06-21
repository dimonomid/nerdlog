package core

import (
	"fmt"
	"strings"

	"github.com/juju/errors"
)

type TransportModeKind string

const (
	TransportModeKindSSHLib = "ssh-lib"
	TransportModeKindSSHBin = "ssh-bin"
	TransportModeKindCustom = "custom"
)

type TransportMode struct {
	kind TransportModeKind

	// customCommand is only relevant when kind == TransportModeKindCustom;
	// it's the external shell command.
	customCommand string
}

func NewTransportModeSSHLib() *TransportMode {
	return &TransportMode{
		kind: TransportModeKindSSHLib,
	}
}

func ParseTransportMode(spec string) (*TransportMode, error) {
	customPrefix := fmt.Sprintf("%s:", TransportModeKindCustom)

	switch {
	case spec == TransportModeKindSSHLib:
		return &TransportMode{
			kind: TransportModeKindSSHLib,
		}, nil

	case spec == TransportModeKindSSHBin:
		return &TransportMode{
			kind: TransportModeKindSSHBin,
		}, nil

	case strings.HasPrefix(spec, customPrefix):
		cmd := strings.TrimPrefix(spec, customPrefix)

		return &TransportMode{
			kind:          TransportModeKindCustom,
			customCommand: cmd,
		}, nil

	default:
		return nil, errors.Errorf("invalid transport mode %q", spec)
	}
}

func (m *TransportMode) Kind() TransportModeKind {
	return m.kind
}

func (m *TransportMode) String() string {
	switch m.kind {
	case TransportModeKindSSHLib, TransportModeKindSSHBin:
		return string(m.kind)
	case TransportModeKindCustom:
		return fmt.Sprintf("%s:%s", m.kind, m.customCommand)
	}

	// Should never be here
	return "invalid"
}
