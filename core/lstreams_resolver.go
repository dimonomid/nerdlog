package core

import (
	"fmt"
	"strings"

	"github.com/dimonomid/nerdlog/shellescape"
	"github.com/dimonomid/ssh_config"
	"github.com/gobwas/glob"
	"github.com/juju/errors"
)

type LStreamsResolver struct {
	params LStreamsResolverParams
}

type LStreamsResolverParams struct {
	// CurOSUser is the current OS username, it's used as the last resort when
	// determining the user for a particular host connection.
	CurOSUser string

	// DefaultTransportMode will be used for all logstreams which don't explicitly
	// define transport.
	DefaultTransportMode *TransportMode

	// ConfigLogStreams is the nerdlog-specific config, typically coming from
	// ~/.config/nerdlog/logstreams.yaml.
	ConfigLogStreams ConfigLogStreams

	// SSHConfig is the general SSH config, typically coming from ~/.ssh/config
	SSHConfig *ssh_config.Config
}

func NewLStreamsResolver(params LStreamsResolverParams) *LStreamsResolver {
	return &LStreamsResolver{
		params: params,
	}
}

type LogStream struct {
	// Name is an arbitrary string which will be included in log messages as the
	// "lstream" context tag; it must uniquely identify the LogStream.
	Name string

	// NOTE: all fields below are shell-specific; so if at some point we want
	// to support other kinds of backends, we'll probably need to factor all
	// these details to a separate struct like LogStreamShell or something.

	// Transport specifies how we can get shell access to the host containing
	// the logstream.
	Transport ConfigLogStreamShellTransport

	// LogFiles contains a list of files which are part of the logstream, like
	// ["/var/log/syslog", "/var/log/syslog.1"]. The [0]th item is the latest log
	// file [1]st is the previous one, etc. One special case here is journalctl:
	// if [0]th item is "journalctl", then we won't use plain log files, and
	// instead will get the data straight from journalctl.
	//
	// It must contain at least a single item, otherwise LogStream is invalid.
	LogFiles []string

	Options LogStreamOptions
}

// ConfigLogStreamShellTransportSSHLib contains params for the ssh transport
// using internal ssh library.
type ConfigLogStreamShellTransportSSHLib struct {
	Host     ConfigHost
	Jumphost *ConfigHost
}

// ConfigLogStreamShellTransportCustomCmd contains params for the custom
// shell command transport.
type ConfigLogStreamShellTransportCustomCmd struct {
	// See description for ShellTransportCustomCmdParams.ShellCommand
	ShellCommand string

	// See description for ShellTransportCustomCmdParams.EnvOverride
	EnvOverride map[string]string
}

type ConfigLogStreamShellTransportLocalhost struct {
	// No details are needed here
}

type ConfigLogStreamShellTransport struct {
	SSHLib    *ConfigLogStreamShellTransportSSHLib
	CustomCmd *ConfigLogStreamShellTransportCustomCmd
	Localhost *ConfigLogStreamShellTransportLocalhost
}

type LogStreamOptions struct {
	SudoMode SudoMode

	// ShellInit can contain arbitrary shell commands which will be executed
	// right after connecting to the host. A common use case is setting
	// custom env vars for tests, like: "export TZ=America/New_York", but
	// might be useful outside of tests as well.
	ShellInit []string
}

// SudoMode can be used to configure nerdlog to read log files with "sudo -n".
// See constants below for more details.
type SudoMode string

const (
	// SudoModeNone is the same as an empty string, and it obviously means that
	// no sudo will be used. It exists as a separate mode to make it possible to
	// override the mode to not use sudo even if some config specifies some
	// non-empty sudo mode.
	SudoModeNone SudoMode = "none"

	// SudoModeFull means that the whole nerdlog_agent.sh script will be executed
	// with "sudo -n". Useful for cases when the log files are owned by root but
	// sudo doesn't require a password.
	SudoModeFull SudoMode = "full"

	// If needed, we might implement something like SudoModeGranular, which would
	// mean that the agent script runs without sudo, but then internally it
	// executes some commands with sudo. It would mean a more complicated setup
	// and more maintenance burden and harder to configure sudoers file, so
	// postponed until we actually need it (if at all).
)

var ValidSudoModes = map[SudoMode]struct{}{
	SudoModeNone: {},
	SudoModeFull: {},
}

type ConfigHost struct {
	// Addr is the address to connect to, in the same format which is used by
	// net.Dial. To copy-paste some docs from net.Dial: the address has the form
	// "host:port". The host must be a literal IP address, or a host name that
	// can be resolved to IP addresses. The port must be a literal port number or
	// a service name.
	//
	// Examples: "golang.org:http", "192.0.2.1:http", "198.51.100.1:22".
	Addr string
	// User is the username to authenticate as.
	User string
}

func (ch *ConfigHost) Key() string {
	return fmt.Sprintf("%s@%s", ch.Addr, ch.User)
}

func (ls LogStream) LogFileLast() string {
	// LogFiles must contain at least a single item, so we don't check anything
	// here, and let it panic naturally if the invariant breaks due to some bug.
	return ls.LogFiles[0]
}

func (ls LogStream) LogFilePrev() (string, bool) {
	if len(ls.LogFiles) >= 2 {
		return ls.LogFiles[1], true
	}

	return "", false
}

// Resolve parses the given logstream spec, and returns the mapping from
// LogStream.Name to the corresponding LogStream. Examples of logstream spec are:
//
// - "myuser@myserver.com:22:/var/log/syslog"
// - "myuser@myserver.com:22"
// - "myuser@myserver.com"
// - "myserver.com"
func (r *LStreamsResolver) Resolve(lstreamsStr string) (map[string]LogStream, error) {
	lstreamsStr = strings.TrimSpace(lstreamsStr)

	parsedLogStreams := map[string]LogStream{}

	// Special case for an empty input: it's allowed and just results in no
	// logstreams.
	if lstreamsStr == "" {
		return parsedLogStreams, nil
	}

	// TODO: when json is supported, splitting by commas will need to be improved.
	parts := strings.Split(lstreamsStr, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)

		if part == "" {
			return nil, errors.Errorf("entry #%d is empty", i+1)
		}

		cfs, err := r.parseLogStreamSpecEntry(part)
		if err != nil {
			return nil, errors.Annotatef(err, "parsing entry #%d (%s)", i+1, part)
		}

		for _, ch := range cfs {
			key := ch.Name

			if _, exists := parsedLogStreams[key]; exists {
				return nil, errors.Errorf("the logstream %s is present at least twice", key)
			}

			parsedLogStreams[key] = ch
		}
	}

	return parsedLogStreams, nil
}

// draftLogStream is a draft version of LogStream; it's used as temporary
// storage in the process of resolving logstreams.
type draftLogStream struct {
	name     string
	host     ConfigHost
	jumphost *ConfigHost
	logFiles []string
	options  ConfigLogStreamOptions
}

// parseLogStreamSpecEntry parses a single logstream spec entry like
// "myuser@myserver.com:22:/var/log/syslog", or "myserver.com", or
// "myserver-*", and returns the corresponding LogStream-s. Note that the spec
// might contain a glob, in which case we might return more than 1 LogStream.
// If the glob didn't match anything, an error is returned.
//
// TODO: it should take a predefined config, to support globs
func (r *LStreamsResolver) parseLogStreamSpecEntry(s string) ([]LogStream, error) {
	parts, err := shellescape.Parse(s)
	if err != nil {
		return nil, errors.Trace(err)
	}

	var plstream *parsedLStream
	var jhconf *ConfigHost
	var logFiles []string

	curFlag := ""
	for _, part := range parts {
		if curFlag == "" && len(part) > 0 && part[0] == '-' {
			curFlag = part
			continue
		}

		switch curFlag {
		case "-J", "--jumphost":
			jhparsed, err := r.parseLStreamStr(part)
			if err != nil {
				return nil, errors.Annotatef(err, "parsing %q as a jumphost", part)
			}

			//if jhparsed.logFileLast != "" || jhparsed.logFilePrev != "" {
			//return nil, errors.Annotatef(err, "jumphost config shouldn't contain files")
			//}

			jhPort := jhparsed.port

			if len(jhparsed.colonParts) > 1 {
				return nil, errors.Errorf("parsing %q as a jumphost: too many colons", part)
			}

			jhconf = &ConfigHost{
				Addr: fmt.Sprintf("%s:%s", jhparsed.hostname, jhPort),
				User: jhparsed.user,
			}

		case "":
			var err error
			plstream, err = r.parseLStreamStr(part)
			if err != nil {
				return nil, errors.Annotatef(err, "parsing %q as a logstream", part)
			}

			if len(plstream.colonParts) > 0 {
				logFiles = append(logFiles, plstream.colonParts[0])
			}

			if len(plstream.colonParts) > 1 {
				logFiles = append(logFiles, plstream.colonParts[1])
			}

			if len(plstream.colonParts) > 2 {
				return nil, errors.Errorf("%q: too many colons", part)
			}
		default:
			return nil, errors.Errorf("invalid flag %s", curFlag)
		}

		curFlag = ""
	}

	if plstream == nil {
		return nil, errors.Errorf("no logstream specified in %q", s)
	}

	lstreams := []draftLogStream{
		{
			name: s,

			host: ConfigHost{
				Addr: fmt.Sprintf("%s:%s", plstream.hostname, plstream.port),
				User: plstream.user,
			},
			jumphost: jhconf,

			logFiles: logFiles,
		},
	}

	// Expand from nerdlog config.
	lstreams, err = expandFromLogStreamsConfig(
		lstreams, r.params.ConfigLogStreams,
	)
	if err != nil {
		return nil, errors.Annotatef(err, "expanding from nerdlog config")
	}

	// Now that we filled things from the nerdlog logstreams config, we should
	// set the default transport mode for all the logstreams which didn't have it
	// specified explicitly in the config.
	//
	// We must do it before expanding things from the ssh config, because
	// expandFromLogStreamsConfig's behavior depends on the Transport: if it's
	// empty or "ssh-lib", then it'll expand connection details (host, port and
	// user); otherwise these connection details will be left intact even if
	// they're empty, because we want to leave all this to whatever external
	// command we'll be using for the transport.
	defaultTransportSpec := r.params.DefaultTransportMode.String()
	for i := range lstreams {
		if lstreams[i].options.Transport == "" {
			lstreams[i].options.Transport = defaultTransportSpec
		}
	}

	// Expand from ssh config.
	lsConfigFromSSHConfig, err := sshConfigToLSConfig(r.params.SSHConfig)
	if err != nil {
		return nil, errors.Annotatef(err, "parsing ssh config")
	}

	lstreams, err = expandFromLogStreamsConfig(
		lstreams, lsConfigFromSSHConfig,
	)
	if err != nil {
		return nil, errors.Annotatef(err, "expanding from ssh config")
	}

	// Set defaults.
	lstreams, err = setLogStreamsConnDefaults(lstreams, r.params.CurOSUser)
	if err != nil {
		return nil, errors.Annotatef(err, "setting defaults")
	}
	lstreams, err = setLogStreamsFileDefaults(lstreams)
	if err != nil {
		return nil, errors.Annotatef(err, "setting defaults")
	}

	// Check if some of the items were clearly indended to be globs matching
	// something (those with asterisks in them), and didn't match anything.
	for _, ls := range lstreams {
		// TODO: would perhaps be useful to implement a function like IsValidDialAddress,
		// which checks a bunch of other things, but for now, a single asterisk check
		// will do.
		if strings.Contains(ls.host.Addr, "*") {
			return nil, errors.Errorf("glob %q didn't match anything (having address %q)", s, ls.host.Addr)
		}
	}

	// Convert draft logstreams to the actual ones.
	ret := make([]LogStream, 0, len(lstreams))
	for _, ls := range lstreams {

		var transport ConfigLogStreamShellTransport
		// Using kinda hackish logic: if the hostname part is "localhost", then
		// ignore the port and user completely, and just use local shell.
		//
		// Maybe we need to treat some other strings similarly, like
		// "localhost.localdomain", or "127.0.0.1", or "::1"; but not sure if it
		// would actually bring any value. So for now, only "localhost" has this
		// special treatment (which is also the default when one opens Nerdlog for
		// the first time).
		if strings.HasPrefix(ls.host.Addr, "localhost:") {
			// Use local shell
			transport = ConfigLogStreamShellTransport{
				Localhost: &ConfigLogStreamShellTransportLocalhost{},
			}
		} else {
			tm, err := ParseTransportMode(ls.options.Transport)
			if err != nil {
				return nil, errors.Annotatef(err, "parsing transport mode for %s", ls.name)
			}

			// Use ssh
			if tm.Kind() == TransportModeKindSSHLib {
				// Use internal ssh library
				transport = ConfigLogStreamShellTransport{
					SSHLib: &ConfigLogStreamShellTransportSSHLib{
						Host:     ls.host,
						Jumphost: ls.jumphost,
					},
				}
			} else {
				// Use external custom command.
				parsedAddr, err := parseAddr(ls.host.Addr)
				if err != nil {
					return nil, errors.Annotatef(err, "parsing addr %s for external custom command", ls.host.Addr)
				}

				envOverride := map[string]string{
					"NLHOST": parsedAddr.host,
				}

				if parsedAddr.port != "" {
					envOverride["NLPORT"] = parsedAddr.port
				}

				if ls.host.User != "" {
					envOverride["NLUSER"] = ls.host.User
				}

				transport = ConfigLogStreamShellTransport{
					CustomCmd: &ConfigLogStreamShellTransportCustomCmd{
						ShellCommand: tm.CustomShellCommand(),
						EnvOverride:  envOverride,
					},
				}
			}
		}

		ret = append(ret, LogStream{
			Name:      ls.name,
			Transport: transport,
			LogFiles:  ls.logFiles,
			Options: LogStreamOptions{
				SudoMode:  ls.options.SudoMode,
				ShellInit: ls.options.ShellInit,
			},
		})
	}

	return ret, nil
}

type parsedLStream struct {
	hostname string
	user     string
	port     string

	colonParts []string
}

func (r *LStreamsResolver) parseLStreamStr(s string) (*parsedLStream, error) {
	// Parsing the logstream descriptor like
	// "user@hostname:/path/to/logfile:/path/to/logfile.1"

	// Parse user, if present
	username := ""
	atIdx := strings.IndexRune(s, '@')
	if atIdx == 0 {
		return nil, errors.Errorf("username is empty")
	} else if atIdx > 0 {
		username = s[:atIdx]
		s = s[atIdx+1:]
	}

	port := ""
	colonParts := []string{}
	parts := strings.Split(s, ":")
	if len(parts) == 0 || parts[0] == "" {
		return nil, errors.Errorf("no hostname")
	}

	if len(parts) > 1 {
		port = parts[1]
	}

	if len(parts) > 2 {
		colonParts = parts[2:]
	}

	return &parsedLStream{
		hostname:   parts[0],
		user:       username,
		port:       port,
		colonParts: colonParts,
	}, nil
}

type ConfigLogStreamWKey struct {
	// Key is the key at which the corresponding ConfigLogStream was
	// stored in the ConfigLogStreams map.
	Key string

	ConfigLogStream
}

// expandFromLogStreamsConfig goes through each of the logstreams, and
// potentially expands every item as per the provided config.
func expandFromLogStreamsConfig(
	logStreams []draftLogStream,
	lsConfig ConfigLogStreams,
) ([]draftLogStream, error) {
	// If there's no config, cut it short.
	if lsConfig == nil {
		return logStreams, nil
	}

	var ret []draftLogStream

	for i, ls := range logStreams {
		var matchedConfigItems []*ConfigLogStreamWKey

		addr, err := parseAddr(ls.host.Addr)
		if err != nil {
			return nil, errors.Annotatef(err, "logstream #%d, parsing address", i+1)
		}

		globPattern := addr.host
		matcher, err := glob.Compile(globPattern)
		if err != nil {
			return nil, errors.Annotatef(err, "logstream #%d, parsing hostname %q as a glob pattern", i+1, addr.host)
		}

		for _, key := range lsConfig.Keys() {
			if matcher.Match(key) {
				matchedConfigItems = append(matchedConfigItems, &ConfigLogStreamWKey{
					Key:             key,
					ConfigLogStream: lsConfig[key],
				})
			}
		}

		// If there's no match, just copy that logstream unchanged.
		if len(matchedConfigItems) == 0 {
			ret = append(ret, ls)
			continue
		}

		// There are some matches, so we need to expand things.
		for _, matchedItem := range matchedConfigItems {
			lsCopy := ls
			addrCopy := addr

			// Always override the name with the key from the config.
			lsCopy.name = strings.Replace(lsCopy.name, globPattern, matchedItem.Key, -1)

			if lsCopy.options.Transport == "" || lsCopy.options.Transport == "ssh-lib" {
				// Overwrite the host address (since what we've had might be a glob):
				// either with the Hostname if it's specified explicitly, or if not, then
				// with the item key.
				if matchedItem.Hostname != "" {
					addrCopy.host = matchedItem.Hostname
				} else {
					addrCopy.host = matchedItem.Key
				}

				// Everything else we'll only override if it's not specified already.

				if addrCopy.port == "" {
					addrCopy.port = matchedItem.Port
				}

				if lsCopy.host.User == "" {
					lsCopy.host.User = matchedItem.User
				}
			} else {
				// Transport is using some external command, so we don't fill in port
				// etc, but we still replace the host with the matched item's key
				// (because or original host might have been a glob)
				addrCopy.host = matchedItem.Key
			}

			// For non-connection details, override them if not specified already.

			if lsCopy.options.SudoMode == "" {
				lsCopy.options.SudoMode = matchedItem.Options.EffectiveSudoMode()
			}

			if lsCopy.options.ShellInit == nil {
				lsCopy.options.ShellInit = matchedItem.Options.ShellInit
			}

			if lsCopy.options.Transport == "" {
				lsCopy.options.Transport = matchedItem.Options.Transport
			}

			if len(lsCopy.logFiles) == 0 {
				lsCopy.logFiles = matchedItem.LogFiles
			}

			lsCopy.host.Addr = fmt.Sprintf("%s:%s", addrCopy.host, addrCopy.port)

			ret = append(ret, lsCopy)
		}
	}

	return ret, nil
}

// setLogStreamsConnDefaults goes through each of the logstreams, and fills in
// missing pieces for which it knows the defaults for how to connect to the
// hosts: port 22, user as the current OS user.
//
// Note that it shouldn't be used when we're utilizing external custom command:
// in this case, we want to leave it all up to that external command.
func setLogStreamsConnDefaults(
	logStreams []draftLogStream,
	osUser string,
) ([]draftLogStream, error) {
	ret := make([]draftLogStream, 0, len(logStreams))

	for i, ls := range logStreams {
		// Only fill in default port and user if the custom transport command is
		// not set; because if it's set, we'll want to defer all the defaults to
		// that external command.
		if ls.options.Transport == "ssh-lib" {
			port, err := portFromAddr(ls.host.Addr)
			if err != nil {
				return nil, errors.Annotatef(err, "logstream #%d, getting port", i+1)
			}

			if port == "" {
				ls.host.Addr += "22"
			}

			if ls.host.User == "" {
				ls.host.User = osUser
			}
		}

		ret = append(ret, ls)
	}

	return ret, nil
}

// setLogStreamsFileDefaults goes through each of the logstreams, and fills in
// missing non-connection pieces, such as default log files.
func setLogStreamsFileDefaults(logStreams []draftLogStream) ([]draftLogStream, error) {
	ret := make([]draftLogStream, 0, len(logStreams))

	for _, ls := range logStreams {
		if len(ls.logFiles) == 0 {
			// Will be autodetected by the agent script.
			ls.logFiles = append(ls.logFiles, "auto")
		}

		if len(ls.logFiles) == 1 {
			// Will be autodetected by the agent script.
			ls.logFiles = append(ls.logFiles, "auto")
		}

		ret = append(ret, ls)
	}

	return ret, nil
}

type parsedAddr struct {
	host string
	port string
}

func parseAddr(addr string) (parsedAddr, error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return parsedAddr{}, errors.Errorf("not a valid addr %q, expected host:port", addr)
	}

	return parsedAddr{
		host: parts[0],
		port: parts[1],
	}, nil
}

// hostnameFromAddr takes an address like net.Dial takes, in the form of
// "host:port", and returns the host part.
func hostnameFromAddr(addr string) (string, error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return "", errors.Errorf("not a valid addr %q, expected host:port", addr)
	}

	return parts[0], nil
}

// portFromAddr takes an address like net.Dial takes, in the form of
// "host:port", and returns the port part.
func portFromAddr(addr string) (string, error) {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return "", errors.Errorf("not a valid addr %q, expected host:port", addr)
	}

	return parts[1], nil
}

func sshConfigToLSConfig(sshConfig *ssh_config.Config) (ConfigLogStreams, error) {
	if sshConfig == nil {
		return nil, nil
	}

	ret := make(ConfigLogStreams, len(sshConfig.Hosts))

	for _, host := range sshConfig.Hosts {
		if len(host.Patterns) == 0 {
			continue
		}

		name := host.Patterns[0].String()
		if name == "" {
			continue
		}

		// If it's a pattern, ignore it
		// (there might be valid use cases where we'd want to use them, but
		// not bothering for now)
		if strings.ContainsAny(name, "*?[]") {
			continue
		}

		hostname, _ := sshConfig.Get(name, "HostName")
		port, _ := sshConfig.Get(name, "Port")
		user, _ := sshConfig.Get(name, "User")

		if hostname == "" && port == "" && user == "" {
			// We can't get anything useful out of this entry anyway, so don't add it
			continue
		}

		ret[name] = ConfigLogStream{
			Hostname: hostname,
			Port:     port,
			User:     user,
		}
	}

	return ret, nil
}
