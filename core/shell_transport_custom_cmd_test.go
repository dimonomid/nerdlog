package core

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestShellTransportCustomCmdPassesEnvOverridesToChildShell verifies the case
// where a custom command starts a real shell which reads NLHOST at runtime.
//
// This differs from a command that references ${NLHOST} directly: those
// variables are expanded while Nerdlog parses the command. Here, the child
// shell must inherit NLHOST through the launched command's environment.
func TestShellTransportCustomCmdPassesEnvOverridesToChildShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the custom shell transport requires /bin/sh")
	}
	// Model a Nerdlog process which happens to have these variables in its own
	// environment. Empty overrides must prevent them leaking into the transport.
	t.Setenv("NLPORT", "ambient-port")
	t.Setenv("NLUSER", "ambient-user")

	transport := NewShellTransportCustomCmd(ShellTransportCustomCmdParams{
		// The first read handles the connection-marker command. Before that, the
		// script reports $1 and its runtime NLPORT/NLUSER values on stderr. $1 is
		// populated by parser-time ${NLPORT:+...} expansion below, while the
		// environment variables are read at runtime. The shell which is exec'd
		// afterward must also inherit the environment overrides.
		ShellCommand: `/bin/sh -c 'printf "%s,%s,%s\n" "$1" "$NLPORT" "$NLUSER" >&2; read command; eval "$command"; exec /bin/sh' parser-default ${NLPORT:+parser-port-$NLPORT}`,
		EnvOverride: map[string]string{
			"NLHOST": "host-from-test",
			"NLPORT": "",
			"NLUSER": "",
		},
	})

	// Model Nerdlog starting the configured transport and completing its marker
	// handshake before it starts sending regular shell commands.
	updatesCh := make(chan ShellConnUpdate, 4)
	transport.Connect(updatesCh)

	var result *ShellConnResult
	deadline := time.After(2 * time.Second)
	for result == nil {
		select {
		case update := <-updatesCh:
			if update.Result != nil {
				result = update.Result
			}
		case <-deadline:
			t.Fatal("timed out waiting for the connection attempt to finish")
		}
	}

	if !assert.NoError(t, result.Err) {
		return
	}
	defer result.Conn.Close()

	// The empty overrides must defeat the ambient values in both parser-time
	// expansion ($1) and the launched process environment (the other fields).
	stderr := bufio.NewScanner(result.Conn.Stderr())
	if !assert.True(t, stderr.Scan(), "expected diagnostic output from the child shell") {
		return
	}
	assert.Equal(t, ",,", stderr.Text())

	// Ask the child shell to reveal the inherited variable. Before the fix this
	// expanded to an ambient (or empty) value instead of the EnvOverride value.
	_, err := fmt.Fprintln(result.Conn.Stdin(), `echo "$NLHOST,$NLPORT,$NLUSER"`)
	if !assert.NoError(t, err) {
		return
	}

	stdout := bufio.NewScanner(result.Conn.Stdout())
	if !assert.True(t, stdout.Scan(), "expected output from the child shell") {
		return
	}
	// Assert that the runtime environment, not only the parser-time expansion,
	// contains the Nerdlog-provided host value.
	assert.Equal(t, "host-from-test,,", stdout.Text())
}

func TestShellTransportCustomCmdTerminatesTimedOutCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the custom shell transport requires /bin/sh")
	}

	pidFile := filepath.Join(t.TempDir(), "command.pid")
	transport := NewShellTransportCustomCmd(ShellTransportCustomCmdParams{
		// The outer single quotes make sure that the custom-command parser leaves
		// $$ for the shell which is eventually started by this command.
		ShellCommand: fmt.Sprintf(
			"/bin/sh -c 'echo $$ > %s; while :; do :; done'", pidFile,
		),
	})

	updatesCh := make(chan ShellConnUpdate, 4)
	transport.Connect(updatesCh)

	var result *ShellConnResult
	deadline := time.After(connectionTimeout + 2*time.Second)
	for result == nil {
		select {
		case update := <-updatesCh:
			if update.Result != nil {
				result = update.Result
			}
		case <-deadline:
			t.Fatal("timed out waiting for the connection attempt to finish")
		}
	}

	assert.EqualError(t, result.Err, "timeout waiting for SSH connection marker")

	pidBytes, err := os.ReadFile(pidFile)
	if !assert.NoError(t, err) {
		return
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if !assert.NoError(t, err) {
		return
	}

	process, err := os.FindProcess(pid)
	if !assert.NoError(t, err) {
		return
	}

	// Signal 0 does not affect the process; it only checks whether it still
	// exists. The command must have been killed and reaped before Connect sends
	// its result.
	assert.Error(t, process.Signal(syscall.Signal(0)))
}
