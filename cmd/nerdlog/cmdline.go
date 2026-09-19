package main

import (
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dimonomid/nerdlog/clipboard"
	"github.com/dimonomid/nerdlog/version"
	"github.com/gdamore/tcell/v2"
	"github.com/juju/errors"
)

// NOTE: handleCmd is always called from the tview's event loop, so it's safe
// to use all UI primitives and nerdlogApp etc.
func (app *nerdlogApp) handleCmd(cmd string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "h", "help":
		var sb strings.Builder
		sb.WriteString("There is no built-in help yet, but check out these resources:\n")
		sb.WriteString("\n")
		sb.WriteString("README.md in the repo:\n    https://github.com/dimonomid/nerdlog\n")
		sb.WriteString("Documentation:\n    https://github.com/dimonomid/nerdlog/blob/master/docs/index.md")

		app.mainView.showMessagebox("err", "Help", sb.String(), &MessageboxParams{
			BackgroundColor: tcell.ColorDarkBlue,
			CopyButton:      true,
		})

	case "time":
		ftr, err := ParseFromToRange(app.options.GetTimezone(), app.params.clock.Now(), strings.Join(parts[1:], " "))
		if err != nil {
			app.printError(err.Error())
			return
		}

		app.mainView.setTimeRange(ftr.From, ftr.To)
		app.mainView.doQuery(doQueryParams{})

	case "w", "write":
		//if len(parts) < 2 {
		//app.printError(":write requires an argument: the filename to write")
		//return
		//}

		//fname := parts[1]

		fname := "/tmp/last_nerdlog"
		if len(parts) >= 2 {
			fname = parts[1]
		}

		if app.lastLogResp == nil {
			app.printError("No logs yet")
			return
		}

		lfile, err := os.Create(fname)
		if err != nil {
			app.printError(fmt.Sprintf("Failed to open %s for writing: %s", fname, err))
			return
		}

		for _, logMsg := range app.lastLogResp.Logs {
			fmt.Fprintf(lfile, "%s <ssh -t %s vim +%d %s>\n",
				logMsg.OrigLine,
				logMsg.Context["lstream"], logMsg.LogLinenumber, logMsg.LogFilename,
			)
		}

		lfile.Close()

		app.printMsg(fmt.Sprintf("Saved to %s", fname))

	case "set":
		if len(parts) < 2 || len(parts[1]) == 0 {
			app.printError("set requires an argument")
			return
		}

		remaining := strings.TrimSpace(cmd[len(parts[0]):])
		setRes, err := app.setOption(remaining)
		if err != nil {
			app.printError(capitalizeFirstRune(err.Error()))
		}

		if setRes != nil {
			if setRes.got != nil {
				optName := setRes.got.optName
				optValue := setRes.got.optValue
				app.printMsg(fmt.Sprintf("%s is %s", optName, optValue))
			}
		}

	case "xc", "xclip":
		qf := app.mainView.getQueryFull()
		shellCmd := qf.MarshalShellCmd()
		if app.params.clipboardInitErr == nil {
			clipboard.WriteText([]byte(shellCmd))
			app.printMsg("Copied to clipboard")
		} else {
			app.printError(fmt.Sprintf("Clipboard is not available: %s", app.params.clipboardInitErr.Error()))
		}

	case "nerdlog":
		// Mimic as if it was called from a shell

		if err := app.unmarshalAndApplyQuery(cmd, doQueryParams{}); err != nil {
			app.printError(err.Error())
			return
		}

	case "prev", "bac", "bck", "back":
		item := app.queryBLHistory.Prev()
		if item == nil {
			app.printError("No more history items")
			return
		}

		if err := app.unmarshalAndApplyQuery(item.Str, doQueryParams{
			dontAddHistoryItem: true,
		}); err != nil {
			// This shouldn't happen really provided a sane history.
			app.printError(err.Error())
			return
		}

		// TODO: print history item stats

	case "next", "fwd", "forward":
		item := app.queryBLHistory.Next()
		if item == nil {
			app.printError("No more history items")
			return
		}

		if err := app.unmarshalAndApplyQuery(item.Str, doQueryParams{
			dontAddHistoryItem: true,
		}); err != nil {
			// This shouldn't happen really provided a sane history.
			app.printError(err.Error())
			return
		}

		// TODO: print history item stats

	case "e", "edit":
		app.mainView.openQueryEditView()

	case "q", "quit":
		app.tviewApp.Stop()

	case "reconnect":
		app.mainView.reconnect(true)

	case "disconnect":
		app.mainView.disconnect()

	case "refresh":
		app.mainView.doQuery(doQueryParams{})

	case "refresh!":
		app.mainView.doQuery(doQueryParams{
			refreshIndex: true,
		})

	case "conndebug", "cdebug":
		app.mainView.showConnDebugInfo()

	case "querydebug", "qdebug", "debug":
		app.mainView.showLastQueryDebugInfo()

	case "querywarnings", "qwarnings", "qwarn":
		if app.lastLogResp == nil || len(app.lastLogResp.Warnings) == 0 {
			// An empty warning list shows the dialog's "No warnings" state.
			app.mainView.handleQueryWarnings(nil, 0, nil, nil)
			return
		}
		warnings := app.lastLogResp.Warnings
		app.mainView.handleQueryWarnings(warnings, app.lastLogResp.NumWarnings, app.queryWarningVisibility(warnings), func(visibility map[string]bool) {
			app.setQueryWarningVisibility(visibility)
		})

	case "version", "about":
		app.mainView.showMessagebox("version", "Version", version.VersionFullDescr(), &MessageboxParams{
			BackgroundColor: tcell.ColorDarkBlue,
			CopyButton:      true,
		})

	default:
		app.printError(fmt.Sprintf("unknown command %q", parts[0]))
	}
}

func (app *nerdlogApp) unmarshalAndApplyQuery(cmd string, dqp doQueryParams) error {
	var qf QueryFull
	if err := qf.UnmarshalShellCmd(cmd); err != nil {
		return errors.Annotatef(err, "parsing")
	}

	if err := app.mainView.applyQueryEditData(qf, dqp); err != nil {
		return errors.Annotatef(err, "applying")
	}

	return nil
}

func capitalizeFirstRune(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
