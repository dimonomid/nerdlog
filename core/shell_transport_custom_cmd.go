package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dimonomid/nerdlog/log"
	"github.com/juju/errors"
	"github.com/mvdan/sh/shell"
)

const echoMarkerConnected = "__CONNECTED__"

// ShellTransportCustomCmd is an implementation of ShellTransport that opens an
// shell session using external custom command (such as ssh).
type ShellTransportCustomCmd struct {
	params ShellTransportCustomCmdParams
}

type ShellTransportCustomCmdParams struct {
	// ShellCommand is a command such as this one:
	// "ssh -o 'BatchMode=yes' ${NLPORT:+-p ${NLPORT}} ${NLUSER:+${NLUSER}@}${NLHOST} /bin/sh"
	//
	// It's interpreted not by an external shell, but https://github.com/mvdan/sh
	// It can use vars from the EnvOverride below, as well as any env vars.
	ShellCommand string

	// EnvOverride overrides env vars.
	//
	// An empty value unsets the one from environment, so e.g. if the environment
	// contains a var FOO=123, but EnvOverride contains they key "FOO" with an
	// empty string, then it'll be interpreted as if FOO just didn't exist.
	//
	// For non-localhost commands, Nerdlog sets 3 env vars here:
	//
	// - "NLHOST": Hosname, always present;
	// - "NLPORT": Port, only present if was specified explicitly or was present
	//   in nerdlog logstreams config.
	// - "NLUSER": Username, only present if was specified explicitly or was
	//   present in nerdlog logstreams config.
	EnvOverride map[string]string

	Logger *log.Logger
}

// NewShellTransportCustomCmd creates a new ShellTransportCustomCmd with the given shell command.
func NewShellTransportCustomCmd(params ShellTransportCustomCmdParams) *ShellTransportCustomCmd {
	params.Logger = params.Logger.WithNamespaceAppended("TransportCustomCmd")

	return &ShellTransportCustomCmd{
		params: params,
	}
}

// Connect starts the local shell and sends the result to the provided channel.
func (s *ShellTransportCustomCmd) Connect(resCh chan<- ShellConnUpdate) {
	go s.doConnect(resCh)
}

func (s *ShellTransportCustomCmd) doConnect(
	resCh chan<- ShellConnUpdate,
) (res ShellConnResult) {
	logger := s.params.Logger

	defer func() {
		if res.Err != nil {
			logger.Errorf("Connection failed: %s", res.Err)
		}
		resCh <- ShellConnUpdate{
			Result: &res,
		}
	}()

	// Parse shell commands into separate fields.
	cmdFields, err := shell.Fields(s.params.ShellCommand, func(varName string) string {
		if value, ok := s.params.EnvOverride[varName]; ok {
			return value
		}

		return os.Getenv(varName)
	})
	if err != nil {
		res.Err = errors.Annotatef(err, "parsing shell command %q", s.params.ShellCommand)
		return res
	}

	if len(cmdFields) == 0 {
		res.Err = errors.Errorf("command is empty")
		return res
	}

	var sshCmdDebugBuilder strings.Builder
	for i, v := range cmdFields {
		if i > 0 {
			sshCmdDebugBuilder.WriteString(" ")
		}
		sshCmdDebugBuilder.WriteString(shellQuote(v))
	}
	sshCmdDebug := sshCmdDebugBuilder.String()

	resCh <- ShellConnUpdate{
		DebugInfo: s.makeDebugInfo(fmt.Sprintf(
			"Trying to connect using external command: %q", sshCmdDebug,
		)),
	}
	logger.Infof("Executing external command: %q", sshCmdDebug)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, cmdFields[0], cmdFields[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, "getting stdin pipe")
		return res
	}
	rawStdout, err := cmd.StdoutPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, "getting stdout pipe")
		return res
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		res.Err = errors.Annotatef(err, "getting stderr pipe")
		return res
	}

	if err := cmd.Start(); err != nil {
		cancel()
		res.Err = errors.Annotatef(err, "starting shell")
		return res
	}

	// Until the connection is successfully returned to the caller, this
	// function owns the command and must clean it up on every error path. In
	// particular, a custom tunnel can hang while connecting; leaving it alive
	// after the marker timeout would make every retry leak another process.
	connectionSucceeded := false
	defer func() {
		if connectionSucceeded {
			return
		}

		_ = stdin.Close()
		cancel()
		_ = cmd.Wait()
	}()

	// To make sure we were able to connect, we just write "echo __CONNECTED__"
	// to stdin, and wait for it to show up in the stdout.

	resCh <- ShellConnUpdate{
		DebugInfo: s.makeDebugInfo(fmt.Sprintf(
			"Command started, writing \"echo %s\", waiting for it in stdout", echoMarkerConnected,
		)),
	}

	_, err = fmt.Fprintf(stdin, "echo %s\n", echoMarkerConnected)
	if err != nil {
		res.Err = errors.Annotatef(err, "writing connection marker")
		return res
	}

	clientStdoutR, clientStdoutW := io.Pipe()
	scanner := bufio.NewScanner(rawStdout)
	// Keep this buffered: after a timeout, doConnect stops receiving from this
	// channel while the scanner goroutine is unwinding after command cleanup.
	connErrCh := make(chan error, 1)
	go func() {
		defer clientStdoutW.Close()
		for scanner.Scan() {
			line := scanner.Text()
			logger.Verbose3f("Got line while looking for connected marker: %s", line)
			if line == echoMarkerConnected {
				logger.Verbose3f("Got the marker, switching to raw passthrough for stdout")
				// Done waiting, switch to raw passthrough
				connErrCh <- nil
				io.Copy(clientStdoutW, rawStdout)
				return
			}
		}
		if err := scanner.Err(); err != nil {
			logger.Errorf("Got scanner error while waiting for connection marker: %s", err.Error())
			connErrCh <- errors.Annotatef(err, "reading from stdout while waiting for connection marker")
		} else {
			// Got EOF while waiting for the marker; apparently ssh failed to connect,
			// so just read up all stderr (which likely contains the actual error message),
			// and return it as an error.
			stderrBytes, _ := io.ReadAll(stderr)
			connErrCh <- errors.Errorf(
				"failed to connect using external command \"%s\": %s",
				sshCmdDebug, string(stderrBytes),
			)
		}
	}()

	// Wait for the marker to show up in output.
	select {
	case err := <-connErrCh:
		if err != nil {
			res.Err = errors.Trace(err)
			return res
		}

		resCh <- ShellConnUpdate{
			DebugInfo: s.makeDebugInfo("Got the marker, connected successfully"),
		}

		// Got the marker, so we're done.
		res.Conn = &ShellConnCustomCmd{
			cmd:    cmd,
			stdin:  stdin,
			stdout: clientStdoutR,
			stderr: stderr,

			ctxCancel: cancel,
		}
		connectionSucceeded = true
		return res

	case <-time.After(connectionTimeout):
		res.Err = errors.New("timeout waiting for SSH connection marker")
		return res
	}
}

func (s *ShellTransportCustomCmd) makeDebugInfo(message string) *ShellConnDebugInfo {
	return &ShellConnDebugInfo{
		Message: message,
	}
}

type ShellConnCustomCmd struct {
	cmd *exec.Cmd

	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader

	ctxCancel context.CancelFunc
}

func (s *ShellConnCustomCmd) Stdin() io.Writer {
	return s.stdin
}

func (s *ShellConnCustomCmd) Stdout() io.Reader {
	return s.stdout
}

func (s *ShellConnCustomCmd) Stderr() io.Reader {
	return s.stderr
}

func (s *ShellConnCustomCmd) Close() {
	// Close stdin; normally this is enough for the external process to finish
	// gracefully.
	s.stdin.Close()

	// Cancel context too, so the external process gets killed (closing stdin is
	// not always enough; e.g. after the OS gets suspended for long enough time,
	// and resumed, the connection keeps hanging without it).
	s.ctxCancel()
}
