package core

import (
	"time"

	"github.com/juju/errors"
)

type lstreamCmd struct {
	// respCh must be either nil, or 1-buffered and it'll receive exactly one
	// message.
	respCh chan lstreamCmdRes

	// Exactly one of the fields below must be non-nil.

	bootstrap *lstreamCmdBootstrap
	ping      *lstreamCmdPing
	queryLogs *lstreamCmdQueryLogs
}

type lstreamCmdCtx struct {
	cmd lstreamCmd

	idx int

	bootstrapCtx *lstreamCmdCtxBootstrap
	pingCtx      *lstreamCmdCtxPing
	queryLogsCtx *lstreamCmdCtxQueryLogs

	// Initially, stdoutDoneIdx and stderrDoneIdx are set to false. Once we
	// receive the "command_done" marker from either stdout or stderr, we set the
	// corresponding bool here to true. Once both are set, we consider the
	// command execution done, and parse the results.
	stdoutDone bool
	stderrDone bool

	// errs contains all errors accumulated during command execution. This
	// includes errors printed by the nerdlog_agent.sh (lines starting from
	// "error:", on either stderr or stdout), as well as any errors generated
	// on the Go side, e.g. failure to parse some other output.
	errs []error

	exitCode string

	// unhandledStdout and unhandledStderr contain the lines which the Go app did
	// not make sense of. These are usually ignored, but if the the
	// nerdlog_agent.sh returns an error code, and there are no specific errors
	// printed (lines with the "error:" prefix), then we'll print all these
	// as an error message.
	unhandledStdout []string
	unhandledStderr []string
}

type lstreamCmdRes struct {
	hostname string

	err  error
	resp interface{}
}

type lstreamCmdBootstrap struct{}

type lstreamCmdCtxBootstrap struct {
	receivedSuccess bool
	receivedFailure bool

	// warnJournalctlNoAdminAccess is set to true if journalctl is used and the
	// user doesn't have access to all the system logs. It's a separate bool
	// instead of a generic warning message to make it possible to suppress it
	// with a flag.
	warnJournalctlNoAdminAccess bool
}

type lstreamCmdPing struct{}

type lstreamCmdCtxPing struct {
}

type lstreamCmdQueryLogs struct {
	maxNumLines int

	from time.Time
	to   time.Time

	query string

	// If linesUntil is not zero, it'll be passed to nerdlog_agent.sh as --lines-until.
	// Effectively, only logs BEFORE this log line (not including it) will be output.
	linesUntil int

	// timestampUntil is not zero, it'll be passed to nerdlog_agent as
	// --timestamp-until-precise and --timestamp-until-seconds. It serves the
	// same purpose as linesUntil for cases when we don't have line numbers (e.g.
	// when using journalctl).
	timestampUntil *timeAndNumMsgs

	// If refreshIndex is true, we'll drop the index file, and rebuild it from
	// scratch (no-op for journalctl logstreams, because there's no
	// nerdlog-maintained index for journalctl).
	refreshIndex bool
}

type lstreamCmdCtxQueryLogs struct {
	Resp *LogResp

	logfiles []logfileWithStartingLinenumber
	lastTime time.Time
}

// maxQueryWarnings limits detailed warnings from one logstream so severe
// corruption cannot produce an enormous response or dialog.
const maxQueryWarnings = 5

// addWarning records a recoverable parsing problem while keeping the detailed
// warning list bounded.
func (ctx *lstreamCmdCtxQueryLogs) addWarning(err error) {
	ctx.Resp.NumWarnings++
	if len(ctx.Resp.Warnings) < maxQueryWarnings {
		ctx.Resp.Warnings = append(ctx.Resp.Warnings, err)
	}
}

// finalizeWarnings adds a summary for malformed records omitted by addWarning.
func (ctx *lstreamCmdCtxQueryLogs) finalizeWarnings() {
	if ctx.Resp.NumWarnings > maxQueryWarnings {
		ctx.Resp.Warnings = append(ctx.Resp.Warnings, errors.Errorf(
			"skipped %d additional malformed log records",
			ctx.Resp.NumWarnings-maxQueryWarnings,
		))
	}
}

type logfileWithStartingLinenumber struct {
	filename       string
	fromLinenumber int
}
