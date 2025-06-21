package core

import "sort"

type ConfigLogStreams map[string]ConfigLogStream

type ConfigLogStream struct {
	// HostAddr is the actual host to connect to.
	//
	// If empty, we'll resort to the ssh config, and if there's no info for that
	// host either, we'll try to get host addr from the key in
	// ConfigLogStreams.LogStreams.
	Hostname string `yaml:"hostname"`

	// Port is the actual port to connect to. If empty, the same overriding rules
	// apply.
	Port string `yaml:"port"`

	// User is the user to authenticate as. If empty, same overriding rules
	// apply.
	User string `yaml:"user"`

	// TODO: optional Jumphost configuration, also with addr and user.

	// LogFiles contains a list of files which are part of the logstream, like
	// ["/var/log/syslog", "/var/log/syslog.1"]. The [0]th item is the latest log
	// file [1]st is the previous one, etc.
	//
	// During the final usage (after resolving everything), it must contain at
	// least a single item, otherwise LogStream is invalid. However in the configs,
	// it's optional (and eventually, if empty, will be set to default values by
	// the LStreamsResolver).
	LogFiles []string `yaml:"log_files"`

	Options ConfigLogStreamOptions `yaml:"options"`
}

// ConfigLogStreamOptions contains additional options for a particular logstream.
type ConfigLogStreamOptions struct {
	// Transport overrides the default transport option; the format is exactly the
	// same as in the transport option: "ssh-lib", "ssh-bin", or "custom:foo bar baz".
	Transport string `yaml:"transport,omitempty"`

	// Sudo is a shortcut for SudoMode: if Sudo is true, it's an equivalent of
	// setting SudoMode to SudoModeFull.
	Sudo bool `yaml:"sudo,omitempty"`

	// SudoMode can be used to configure nerdlog to read log files with "sudo -n".
	// See constants for the SudoMode type for more details.
	SudoMode SudoMode `yaml:"sudo_mode,omitempty"`

	// ShellInit can contain arbitrary shell commands which will be executed
	// right after connecting to the host. A common use case is setting
	// custom env vars for tests, like: "export TZ=America/New_York", but
	// might be useful outside of tests as well.
	ShellInit []string `yaml:"shell_init,omitempty"`
}

func (lss ConfigLogStreams) Keys() []string {
	keys := make([]string, 0, len(lss))
	for k := range lss {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// EffectiveSudoMode returns the SudoMode considering all fields that can
// affect it: Sudo and SudoMode.
func (opts ConfigLogStreamOptions) EffectiveSudoMode() SudoMode {
	if opts.SudoMode != "" {
		return opts.SudoMode
	}

	if opts.Sudo {
		return SudoModeFull
	}

	return ""
}
