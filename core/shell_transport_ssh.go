package core

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dimonomid/nerdlog/log"
	"github.com/juju/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// ShellTransportSSH implements ShellTransport over SSH.
type ShellTransportSSH struct {
	params ShellTransportSSHParams
}

var _ ShellTransport = &ShellTransportSSH{}

func NewShellTransportSSH(params ShellTransportSSHParams) *ShellTransportSSH {
	params.Logger = params.Logger.WithNamespaceAppended("TransportSSH")

	return &ShellTransportSSH{
		params: params,
	}
}

type ShellTransportSSHParams struct {
	// SSHKeys specifies paths to ssh keys to try, in the given order, until
	// an existing key is found.
	SSHKeys []string

	ConnDetails ConfigLogStreamShellTransportSSH

	Logger *log.Logger

	// EphemeralKeyProvider optionally provides ephemeral SSH keys for authentication.
	EphemeralKeyProvider EphemeralKeyProvider
}

func (st *ShellTransportSSH) Connect(resCh chan<- ShellConnUpdate) {
	go st.doConnect(resCh)
}

func (st *ShellTransportSSH) doConnect(
	resCh chan<- ShellConnUpdate,
) (res ShellConnResult) {
	logger := st.params.Logger

	defer func() {
		if res.Err != nil {
			logger.Errorf("Connection failed: %s", res.Err)
		}

		resCh <- ShellConnUpdate{
			Result: &res,
		}
	}()

	connDetails := st.params.ConnDetails

	var sshClient *ssh.Client

	conf, err := st.getClientConfig(resCh, logger, connDetails.Host.User)
	if err != nil {
		res.Err = errors.Annotatef(err, "getting ssh client for %s", connDetails.Host.User)
		return res
	}

	if connDetails.Jumphost != nil {
		logger.Infof("Connecting via jumphost")
		// Use jumphost
		jumphost, err := st.getJumphostClient(resCh, logger, connDetails.Jumphost)
		if err != nil {
			logger.Errorf("Jumphost connection failed: %s", err)
			res.Err = errors.Annotatef(err, "getting jumphost client")
			return res
		}

		conn, err := dialWithTimeout(jumphost, "tcp", connDetails.Host.Addr, connectionTimeout)
		if err != nil {
			res.Err = errors.Annotatef(err, conf.Descr)
			return res
		}

		authConn, chans, reqs, err := ssh.NewClientConn(conn, connDetails.Host.Addr, conf.ClientConfig)
		if err != nil {
			res.Err = errors.Annotatef(err, conf.Descr)
			return res
		}

		sshClient = ssh.NewClient(authConn, chans, reqs)
	} else {
		logger.Infof("Connecting to %s (%+v)", connDetails.Host.Addr, conf)
		var err error
		sshClient, err = ssh.Dial("tcp", connDetails.Host.Addr, conf.ClientConfig)
		if err != nil {
			res.Err = errors.Annotatef(err, conf.Descr)
			return res
		}
	}

	logger.Infof("Connected to %s", connDetails.Host.Addr)

	sshSession, err := sshClient.NewSession()
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	stdinBuf, err := sshSession.StdinPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	stdoutBuf, err := sshSession.StdoutPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	stderrBuf, err := sshSession.StderrPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	err = sshSession.Start("/bin/sh")
	if err != nil {
		res.Err = errors.Annotatef(err, conf.Descr)
		return res
	}

	res.Conn = &ShellConnSSH{
		sshClient:  sshClient,
		sshSession: sshSession,

		stdinBuf:  stdinBuf,
		stdoutBuf: stdoutBuf,
		stderrBuf: stderrBuf,
	}

	return res
}

// dialWithTimeout is a hack needed to get a timeout for the ssh client.
// https://stackoverflow.com/questions/31554196/ssh-connection-timeout
//
// It's possible we could accomplish the same thing by using NewClient() with Conn.SetDeadline(), but that requires
// some refactoring.
func dialWithTimeout(client *ssh.Client, protocol, hostAddr string, timeout time.Duration) (net.Conn, error) {
	finishedChan := make(chan net.Conn)
	errChan := make(chan error)
	go func() {
		conn, err := client.Dial(protocol, hostAddr)
		if err != nil {
			errChan <- err
			return
		}
		finishedChan <- conn
	}()

	select {
	case conn := <-finishedChan:
		return conn, nil

	case err := <-errChan:
		return nil, errors.Trace(err)

	case <-time.After(connectionTimeout):
		// Don't close the connection here since it's reused
		return nil, errors.New("ssh client dial timed out")
	}
}

type ClientConfigWMeta struct {
	// ClientConfig is the actual client config.
	ClientConfig *ssh.ClientConfig

	// Descr is a human-readable string which is useful to include in any
	// error messages about this SSH connection.
	Descr string
}

type AuthMethodWMeta struct {
	AuthMethod ssh.AuthMethod

	// Descr is a human-readable string which is useful to include in any
	// error messages about this SSH connection.
	Descr string
}

func (st *ShellTransportSSH) getClientConfig(resCh chan<- ShellConnUpdate, logger *log.Logger, username string) (*ClientConfigWMeta, error) {
	auth, err := st.getSSHAuthMethod(resCh, logger)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return &ClientConfigWMeta{
		ClientConfig: &ssh.ClientConfig{
			User: username,
			Auth: []ssh.AuthMethod{auth.AuthMethod},

			// TODO: fix it
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),

			Timeout: connectionTimeout,
		},
		Descr: auth.Descr,
	}, nil
}

var (
	sshAuthMethodShared    *AuthMethodWMeta
	sshAuthMethodSharedMtx sync.Mutex
)

func (st *ShellTransportSSH) getSSHAuthMethod(resCh chan<- ShellConnUpdate, logger *log.Logger) (*AuthMethodWMeta, error) {
	sshAuthMethodSharedMtx.Lock()
	defer sshAuthMethodSharedMtx.Unlock()

	if sshAuthMethodShared != nil {
		return sshAuthMethodShared, nil
	}

	// Try ephemeral key provider first
	if st.params.EphemeralKeyProvider != nil {
		authMethodIface, err := st.params.EphemeralKeyProvider.GetAuthMethod()
		if err == nil {
			authMethod, ok := authMethodIface.(ssh.AuthMethod)
			if !ok {
				logger.Infof("Ephemeral SSH key provider returned invalid AuthMethod type")
			} else {
				logger.Infof("Using ephemeral SSH key for authentication")
				sshAuthMethodShared = &AuthMethodWMeta{
					AuthMethod: authMethod,
					Descr:      "using ephemeral SSH key",
				}
				return sshAuthMethodShared, nil
			}
		}
		logger.Infof("Ephemeral SSH key not available: %v", err)
	}

	// Try ssh-agent next
	var sshAgentErr error
	sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock != "" {
		logger.Infof("Trying ssh-agent via SSH_AUTH_SOCK=%s", sshAuthSock)
		sshAgent, err := net.Dial("unix", sshAuthSock)
		if err != nil {
			logger.Infof("Failed to connect to ssh-agent: %s", err.Error())
			sshAgentErr = errors.Annotatef(err, "using SSH_AUTH_SOCK env var")
		} else {
			sshAuthMethodShared = &AuthMethodWMeta{
				AuthMethod: ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers),
				Descr:      "using ssh-agent",
			}
			return sshAuthMethodShared, nil
		}
	} else {
		sshAgentErr = errors.Errorf("SSH_AUTH_SOCK env var is empty")
		logger.Infof("SSH_AUTH_SOCK not set; skipping ssh-agent")
	}

	// Fall back to private key
	logger.Infof("Fallback to parsing ssh key...")

	var keyPath string
	var keyData []byte
	var errBuilder strings.Builder
	foundKey := false
	for _, keyPath = range st.params.SSHKeys {
		var err error
		keyData, err = os.ReadFile(keyPath)
		if err != nil {
			if errBuilder.Len() > 0 {
				errBuilder.WriteString(", ")
			}
			errBuilder.WriteString(fmt.Sprintf("%s: %s", keyPath, err.Error()))
			continue
		}

		// Found the key file.
		foundKey = true
		break
	}

	if !foundKey {
		errMsg := fmt.Sprintf(
			"failed to read key data from any of the following: %s (%s)",
			st.params.SSHKeys,
			errBuilder.String(),
		)
		// Return error if ephemeral key provider is not set or also failed
		if st.params.EphemeralKeyProvider == nil {
			return nil, errors.New(errMsg)
		}
		// If ephemeral key provider is set, return error only if it also failed
		if st.params.EphemeralKeyProvider != nil {
			_, err := st.params.EphemeralKeyProvider.GetAuthMethod()
			if err != nil {
				return nil, errors.New(errMsg)
			}
		}
	} else if len(keyData) == 0 {
		// No keys and no ephemeral key provider, return error
		return nil, errors.New("no SSH keys found and no ephemeral key available")
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		if _, ok := err.(*ssh.PassphraseMissingError); ok {
			// We need a passphrase to decrypt the private key. Request it from
			// the client code.
			passphraseCh := make(chan string, 1)

			resCh <- ShellConnUpdate{
				DataRequest: &ShellConnDataRequest{
					Title:      "SSH key is passphrase-protected",
					Message:    fmt.Sprintf("Unable to use ssh-agent: %s, falling back to ssh keys.\nPlease enter passphrase for %s.\nAlternatively, use ssh-agent, and make sure the SSH_AUTH_SOCK environment variable is set correctly.\nTo use a different ssh key, provide it with the --ssh-key flag.", sshAgentErr.Error(), keyPath),
					DataKind:   ShellConnDataKindPassword,
					ResponseCh: passphraseCh,
				},
			}

			// Now wait for the client code to provide the passphrase.
			//
			// TODO: support teardown; as of now, if the user tries to exit the app,
			// it'll be stuck on the "Closing connections" stage, until the Ctrl+C is
			// pressed.
			passphrase := <-passphraseCh

			var err error
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(passphrase))
			if err != nil {
				// Something has failed even with the provided passphrase.
				// We don't implement any retries here in case of typos, because the
				// whole connection will be retried, and we'll naturally ask for the
				// passphrase again.
				return nil, errors.Annotatef(err, "parsing private key from %s with the given passphrase", keyPath)
			}
		} else {
			return nil, errors.Annotatef(err, "parsing private key from %s", keyPath)
		}
	}

	logger.Infof("Using private key from %s", keyPath)
	sshAuthMethodShared = &AuthMethodWMeta{
		AuthMethod: ssh.PublicKeys(signer),
		Descr:      fmt.Sprintf("using key %s", keyPath),
	}
	return sshAuthMethodShared, nil
}

var (
	jumphostsShared    = map[string]*ssh.Client{}
	jumphostsSharedMtx sync.Mutex
)

func (st *ShellTransportSSH) getJumphostClient(resCh chan<- ShellConnUpdate, logger *log.Logger, jhConfig *ConfigHost) (*ssh.Client, error) {
	jumphostsSharedMtx.Lock()
	defer jumphostsSharedMtx.Unlock()

	key := jhConfig.Key()
	jh := jumphostsShared[key]
	if jh == nil {
		logger.Infof("Connecting to jumphost... %+v", jhConfig)

		parts := strings.Split(jhConfig.Addr, ":")
		if len(parts) != 2 {
			return nil, errors.Errorf("malformed jumphost address %q", jhConfig.Addr)
		}

		addrs, err := net.LookupHost(parts[0])
		if err != nil {
			return nil, errors.Trace(err)
		}

		if len(addrs) != 1 {
			return nil, errors.New("Address not found")
		}

		conf, err := st.getClientConfig(resCh, logger, jhConfig.User)
		if err != nil {
			return nil, errors.Trace(err)
		}

		jh, err = ssh.Dial("tcp", jhConfig.Addr, conf.ClientConfig)
		if err != nil {
			return nil, errors.Trace(err)
		}

		jumphostsShared[key] = jh

		logger.Infof("Jumphost ok")
	}

	return jh, nil
}

// ShellConnSSH implements ShellConn for SSH.
type ShellConnSSH struct {
	sshClient  *ssh.Client
	sshSession *ssh.Session

	stdinBuf  io.WriteCloser
	stdoutBuf io.Reader
	stderrBuf io.Reader
}

var _ ShellConn = &ShellConnSSH{}

func (c *ShellConnSSH) Stdin() io.Writer {
	return c.stdinBuf
}

func (c *ShellConnSSH) Stdout() io.Reader {
	return c.stdoutBuf
}

func (c *ShellConnSSH) Stderr() io.Reader {
	return c.stderrBuf
}

// Close closes underlying SSH connection.
func (c *ShellConnSSH) Close() {
	c.stdinBuf.Close()
	c.sshSession.Close()
	c.sshClient.Close()
}
