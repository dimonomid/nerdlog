package core

import (
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
