package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dimonomid/clock"
	"github.com/dimonomid/nerdlog/blhistory"
	"github.com/dimonomid/nerdlog/clhistory"
	"github.com/dimonomid/nerdlog/core"
	"github.com/dimonomid/nerdlog/log"
	"github.com/dimonomid/ssh_config"
	"github.com/juju/errors"
	"github.com/rivo/tview"
)

type nerdlogApp struct {
	params nerdlogAppParams

	options *OptionsShared

	// tviewApp is the TUI application. NOTE: once TUI exits, tviewApp is reset
	// to nil.
	tviewApp *tview.Application

	lsman    *core.LStreamsManager
	mainView *MainView

	// cmdLineHistory is the command line history
	cmdLineHistory *clhistory.CLHistory

	// queryBLHistory is the history of queries, as shell strings like this:
	// - nerdlog --lstreams 'localhost' --time -10h --pattern '/something/'
	// - nerdlog --lstreams 'localhost' --time -2h --pattern '/something/'
	queryBLHistory *blhistory.BLHistory
	// queryCLHistory is tracking the same data as queryBLHistory (queries like
	// nerdlog --lstreams .....), but it's command-line-like, and it can be
	// navigated on the query edit form.
	queryCLHistory *clhistory.CLHistory

	lastQueryFull QueryFull

	// lastLogResp contains the last response from LStreamsManager.
	lastLogResp *core.LogRespTotal
}

type nerdlogAppParams struct {
	initialQueryData QueryFull
	connectRightAway bool
	clipboardInitErr error
	logLevel         log.LogLevel
	sshConfigPath    string
	sshKeys          []string

	noJournalctlAccessWarn bool

	// EphemeralKeyProvider specifies which ephemeral key provider to use.
	EphemeralKeyProvider string
}

type cmdWithOpts struct {
	cmd  string
	opts CmdOpts
}

func newNerdlogApp(
	params nerdlogAppParams, queryCLHistory *clhistory.CLHistory,
) (*nerdlogApp, error) {
	logger := log.NewLogger(params.logLevel)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Annotatef(err, "getting home dir")
	}

	cmdLineHistory, err := clhistory.New(clhistory.CLHistoryParams{
		Filename: filepath.Join(homeDir, ".nerdlog_history"),
	})
	if err != nil {
		return nil, errors.Annotatef(err, "initializing cmdline history")
	}

	app := &nerdlogApp{
		params: params,

		options: NewOptionsShared(Options{
			Timezone:             time.Local,
			MaxNumLines:          250,
			EphemeralKeyProvider: params.EphemeralKeyProvider,
		}),

		tviewApp: tview.NewApplication(),

		cmdLineHistory: cmdLineHistory,
		queryBLHistory: blhistory.New(),
		queryCLHistory: queryCLHistory,
	}

	cmdCh := make(chan cmdWithOpts, 8)

	app.mainView = NewMainView(&MainViewParams{
		App:     app.tviewApp,
		Options: app.options,
		OnLogQuery: func(params core.QueryLogsParams) {
			params.MaxNumLines = app.options.GetMaxNumLines()

			// Get the current QueryFull and marshal it to a shell command.
			qf := app.mainView.getQueryFull()
			qfStr := qf.MarshalShellCmd()

			// Add this query shell command to the commandline-like history.
			app.queryCLHistory.Add(qfStr)

			// If needed, also add it to the browser-like history.
			if qf != app.lastQueryFull {
				app.lastQueryFull = qf
				if !params.DontAddHistoryItem {
					app.queryBLHistory.Add(qfStr)
				}
			}

			app.lsman.QueryLogs(params)
		},
		OnLStreamsChange: func(lstreamsSpec string) error {
			err := app.lsman.SetLStreams(lstreamsSpec)
			if err != nil {
				return errors.Trace(err)
			}

			return nil
		},
		OnDisconnectRequest: func() {
			app.lsman.Disconnect()
		},
		OnReconnectRequest: func() {
			app.lsman.Reconnect()
		},
		OnCmd: func(cmd string, opts CmdOpts) {
			cmdCh <- cmdWithOpts{
				cmd:  cmd,
				opts: opts,
			}
		},

		CmdHistory:   app.cmdLineHistory,
		QueryHistory: app.queryCLHistory,

		Logger: logger,
	})

	// NOTE: initLStreamsManager has to be called _after_ app.mainView is initialized.
	if err := app.initLStreamsManager(params, "", homeDir, logger); err != nil {
		return nil, errors.Trace(err)
	}

	if !params.connectRightAway {
		app.mainView.params.App.SetFocus(app.mainView.logsTable)
		app.mainView.queryEditView.Show(params.initialQueryData)
	} else {
		if err := app.mainView.applyQueryEditData(params.initialQueryData, doQueryParams{}); err != nil {
			panic(err.Error())
		}
	}

	go app.handleCmdLine(cmdCh)

	return app, nil
}

func (app *nerdlogApp) runTViewApp() error {
	err := app.tviewApp.SetRoot(app.mainView.GetUIPrimitive(), true).Run()

	// Now that TUI app has finished, remember that by resetting it to nil.
	app.tviewApp = nil

	return err
}

// createEphemeralKeyProvider creates an ephemeral key provider based on the configuration.
func createEphemeralKeyProvider(providerType string) core.EphemeralKeyProvider {
	switch providerType {
	case "mock":
		return &core.EphemeralKeyProviderMock{}
	case "opkssh":
		return &core.EphemeralKeyProviderOpkssh{}
	default:
		return &core.DummyEphemeralKeyProvider{}
	}
}

// NOTE: initLStreamsManager has to be called _after_ app.mainView is initialized.
func (app *nerdlogApp) initLStreamsManager(
	params nerdlogAppParams,
	initialLStreams string,
	homeDir string,
	logger *log.Logger,
) error {
	updatesCh := make(chan core.LStreamsManagerUpdate, 128)
	go func() {
		// We don't want to necessarily update UI on _every_ state update, since
		// they might be getting a lot of those messages due to those progress
		// percentage updates; so we just remember the last state, and only update
		// the UI once we don't have more messages yet.
		var lastState *core.LStreamsManagerState
		var logResps []*core.LogRespTotal // TODO: perhaps we should also only keep the last one?
		var bootstrapErrors []error
		var bootstrapWarnings []error
		var dataRequests []*core.ShellConnDataRequest

		handleUpdate := func(upd core.LStreamsManagerUpdate) {
			switch {
			case upd.State != nil:
				lastState = upd.State
			case upd.LogResp != nil:
				logResps = append(logResps, upd.LogResp)
			case upd.BootstrapIssue != nil:
				if upd.BootstrapIssue.Err != "" {
					bootstrapErrors = append(
						bootstrapErrors,
						errors.Errorf("%s: %s", upd.BootstrapIssue.LStreamName, upd.BootstrapIssue.Err),
					)
				}

				if upd.BootstrapIssue.WarnJournalctlNoAdminAccess && !params.noJournalctlAccessWarn {
					bootstrapWarnings = append(
						bootstrapWarnings,
						errors.Errorf("%s: journalctl is being used, but the user doesn't have access to all the system logs (not it adm or systemd-journal groups, and not root).\n\nUse --no-journalctl-access-warning to suppress this message.", upd.BootstrapIssue.LStreamName),
					)
				}

			case upd.DataRequest != nil:
				dataRequests = append(dataRequests, upd.DataRequest)

			default:
				panic("empty lstreams manager update")
			}
		}

		for {
			select {
			case upd := <-updatesCh:
				handleUpdate(upd)

			default:
				// If anything has changed, update the UI.
				//
				// The tviewApp might be nil here if the TUI app has finished, but we're
				// still receiving updates during the teardown; so if that's the case,
				// just don't update the TUI.
				if app.tviewApp != nil &&
					(lastState != nil ||
						len(logResps) > 0 ||
						len(bootstrapErrors) > 0 ||
						len(bootstrapWarnings) > 0 ||
						len(dataRequests) > 0) {

					app.tviewApp.QueueUpdateDraw(func() {
						if lastState != nil {
							app.mainView.applyHMState(lastState)
						}

						for _, logResp := range logResps {
							if len(logResp.Errs) > 0 {
								app.mainView.handleQueryError(combineErrors(logResp.Errs))
								return
							}

							app.mainView.applyLogs(logResp)
							app.lastLogResp = logResp
						}

						if len(bootstrapErrors) > 0 {
							app.mainView.handleBootstrapError(combineErrors(bootstrapErrors))
						}

						if len(bootstrapWarnings) > 0 {
							app.mainView.handleBootstrapWarning(combineErrors(bootstrapWarnings))
						}

						for _, dataReq := range dataRequests {
							app.mainView.handleDataRequest(dataReq)
						}
					})

					lastState = nil
					logResps = nil
					bootstrapErrors = nil
					bootstrapWarnings = nil
					dataRequests = nil
				}

				// The same select again, but without the default case.
				select {
				case upd := <-updatesCh:
					handleUpdate(upd)
				}
			}
		}
	}()

	envUser := os.Getenv("USER")

	var logstreamsCfg core.ConfigLogStreams
	logstreamsCfgPath := filepath.Join(homeDir, ".config", "nerdlog", "logstreams.yaml")
	_, statErr := os.Stat(logstreamsCfgPath)
	if statErr == nil {
		appLogstreamsCfg, err := LoadLogstreamsConfigFromFile(logstreamsCfgPath)
		if err != nil {
			return errors.Trace(err)
		}

		logstreamsCfg = appLogstreamsCfg.LogStreams
	}

	var sshConfig *ssh_config.Config
	if params.sshConfigPath != "" {
		file, err := os.Open(params.sshConfigPath)
		if err != nil {
			return errors.Annotatef(
				err,
				"opening ssh config from %s (path is configurable via --ssh-config)",
				params.sshConfigPath,
			)
		}
		defer file.Close()

		sshConfig, err = ssh_config.Decode(file)
		if err != nil {
			return errors.Annotatef(
				err,
				"parsing ssh config from %s (path is configurable via --ssh-config)",
				params.sshConfigPath,
			)
		}
	}

	// Create ephemeral key provider
	ephemeralKeyProvider := createEphemeralKeyProvider(params.EphemeralKeyProvider)

	app.lsman = core.NewLStreamsManager(core.LStreamsManagerParams{
		Logger: logger,

		ConfigLogStreams: logstreamsCfg,
		SSHConfig:        sshConfig,
		SSHKeys:          params.sshKeys,

		InitialLStreams: initialLStreams,

		ClientID: envUser,

		UpdatesCh: updatesCh,

		Clock: clock.New(),

		EphemeralKeyProvider: ephemeralKeyProvider,
	})

	return nil
}

func (app *nerdlogApp) handleCmdLine(cmdCh <-chan cmdWithOpts) {
	for {
		cwo := <-cmdCh
		app.tviewApp.QueueUpdateDraw(func() {
			if !cwo.opts.Internal {
				app.cmdLineHistory.Add(cwo.cmd)
			}
			app.handleCmd(cwo.cmd)
			app.mainView.formatTimeRange()
			app.mainView.formatLogs()
		})
	}
}

// printError lets user know that there is an error by printing a simple error
// message over the command line, sort of like in Vim.
// Note that if command line is focused atm, the message will not be printed
// and it's a no-op.
func (app *nerdlogApp) printError(msg string) {
	app.mainView.printMsg(msg, nlMsgLevelErr)
}

// printMsg prints a FYI kind of message. Also see notes for printError.
func (app *nerdlogApp) printMsg(msg string) {
	app.mainView.printMsg(msg, nlMsgLevelInfo)
}

func (app *nerdlogApp) Close() {
	app.lsman.Close()
}

func (app *nerdlogApp) Wait() {
	app.lsman.Wait()
}

func combineErrors(errs []error) error {
	var totalErr error
	if len(errs) == 1 {
		totalErr = errs[0]
	} else {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d errors:", len(errs)))
		for i, curErr := range errs {
			sb.WriteString("\n")
			sb.WriteString(fmt.Sprintf("%d: %s", i+1, curErr.Error()))
		}
		totalErr = errors.New(sb.String())
	}

	return totalErr
}
