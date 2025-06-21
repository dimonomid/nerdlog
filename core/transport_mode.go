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

func NewTransportModeSSHBin() *TransportMode {
	return &TransportMode{
		kind: TransportModeKindSSHBin,
	}
}

func NewTransportModeCustom(customCommand string) *TransportMode {
	return &TransportMode{
		kind:          TransportModeKindCustom,
		customCommand: customCommand,
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

func (m *TransportMode) CustomShellCommand() string {
	switch m.kind {
	case TransportModeKindSSHLib:
		return ""
	case TransportModeKindSSHBin:
		return DefaultSSHShellCommand
	case TransportModeKindCustom:
		return m.customCommand
	}

	panic("should never be here")
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

// DefaultSSHShellCommand is a custom shell command which is used with ssh-bin
// transport.
//
// It's interpreted not by an external shell, but by https://github.com/mvdan/sh.
//
// Vars NLHOST, NLPORT and NLUSER are set by the nerdlog internally, but it can
// also use arbitrary environment vars.
const DefaultSSHShellCommand = "ssh -o 'BatchMode=yes' ${NLPORT:+-p ${NLPORT}} ${NLUSER:+${NLUSER}@}${NLHOST} /bin/sh"
