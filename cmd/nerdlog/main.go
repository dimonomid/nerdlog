package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dimonomid/nerdlog/clhistory"
	"github.com/dimonomid/nerdlog/clipboard"
	"github.com/dimonomid/nerdlog/log"
	"github.com/dimonomid/nerdlog/version"
	"github.com/spf13/pflag"
)

// TODO: make multiple of them
const inputTimeLayout = "Jan2 15:04"
const inputTimeLayoutMMHH = "15:04"

func main() {
	appClock, err := newAppClock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing clock: %s\n", err)
		os.Exit(1)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting home dir: %s\n", err)
		os.Exit(1)
	}

	defaultSSHKeys := []string{
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_ecdsa"),
		filepath.Join(homeDir, ".ssh", "id_rsa"),
	}

	var (
		flagVersion = pflag.BoolP("version", "v", false, "Print version info and exit")

		flagTime              = pflag.StringP("time", "t", "", "Time range in the same format as accepted by the UI. Examples: '1h', 'Mar27 12:00'")
		flagLStreamsConfig    = pflag.String("lstreams-config", filepath.Join(homeDir, ".config", "nerdlog", "logstreams.yaml"), "logstreams config file to use; set to an empty string to disable reading logstreams config")
		flagCmdHistoryFile    = pflag.String("cmdhistory-file", filepath.Join(homeDir, ".nerdlog_history"), "Command-line history file")
		flagSearchHistoryFile = pflag.String("searchhistory-file", filepath.Join(homeDir, ".nerdlog_search_history"), "Table search history file")
		flagQueryHistoryFile  = pflag.String("queryhistory-file", filepath.Join(homeDir, ".nerdlog_query_history"), "Query history file")
		flagLStreams          = pflag.StringP("lstreams", "h", "", "Logstreams to connect to, as comma-separated glob patterns, e.g. 'foo-*,bar-*'")
		flagQuery             = pflag.StringP("pattern", "p", "", "Initial awk pattern to use")
		flagSelectQuery       = pflag.StringP("selquery", "s", "", "SELECT-like query to specify which fields to show, like 'time STICKY, message, lstream, level_name AS level, *'")
		flagLogLevel          = pflag.String("loglevel", "error", "This is NOT about the logs that nerdlog fetches from the remote servers, it's rather about nerdlog's own log. Valid values are: error, warning, info, verbose1, verbose2 or verbose3")
		flagSSHConfig         = pflag.String("ssh-config", filepath.Join(homeDir, ".ssh", "config"), "ssh config file to use; set to an empty string to disable reading ssh config")
		flagSSHKeys           = pflag.StringSlice("ssh-key", defaultSSHKeys, "ssh keys to use; only the first existing file will be used")

		// NOTE: we specifically use StringArray and not StringSlice here, because we
		// don't want it to interpret commas in the values, like "--set foo=123,bar=234", since
		// it messes with more complicated option syntax like 'transport=custom:some "arbitrary command"'
		flagSet = pflag.StringArray("set", []string{}, "Initial option values in the form option=value, in the same way you'd specify them for the :set command. This flag can be given multiple times")

		flagNoJournalctlAccessWarn = pflag.Bool("no-journalctl-access-warning", false, "Suppress the warning when journalctl is being used by the user who can't read all system logs")
	)

	pflag.Parse()

	if *flagVersion {
		fmt.Print(version.VersionFullDescr())
		os.Exit(0)
	}

	queryCLHistory, err := clhistory.New(clhistory.CLHistoryParams{
		Filename: *flagQueryHistoryFile,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing query history: %s\n", err)
		os.Exit(1)
	}

	initialTime := "-1h"
	initialLStreams := "localhost"
	if runtime.GOOS == "windows" {
		// On Windows, "localhost" doesn't make much sense, since there are usually no
		// plain log files and no journalctl, so using a different default here.
		initialLStreams = "myserver.com:22"
	}
	initialQuery := ""
	initialSelectQuery := DefaultSelectQuery
	connectRightAway := false

	if *flagTime != "" {
		initialTime = *flagTime
		connectRightAway = true
	}

	if *flagLStreams != "" {
		initialLStreams = *flagLStreams
		connectRightAway = true
	}

	if *flagQuery != "" {
		initialQuery = *flagQuery
		connectRightAway = true
	}

	if *flagSelectQuery != "" {
		initialSelectQuery = SelectQuery(*flagSelectQuery)
		connectRightAway = true
	}

	initialQueryData := QueryFull{
		Time:        initialTime,
		Query:       initialQuery,
		LStreams:    initialLStreams,
		SelectQuery: initialSelectQuery,
	}

	if !connectRightAway {
		// No query params were given, try to get the last one from the history.
		item, _ := queryCLHistory.Prev("")
		if item.Str != "" {
			var qf QueryFull
			if err := qf.UnmarshalShellCmd(item.Str); err != nil {
				// Ignore the error, just use the defaults
			} else {
				// Successfully parsed the last item from query history, use that.
				initialQueryData = qf
			}
		}
	}

	if clipboard.InitErr != nil {
		fmt.Printf("NOTE: X Clipboard is not available: %s\n", clipboard.InitErr.Error())
	}

	logLevel := log.Info
	if *flagLogLevel == "error" {
		logLevel = log.Error
	} else if *flagLogLevel == "warning" {
		logLevel = log.Warning
	} else if *flagLogLevel == "info" {
		logLevel = log.Info
	} else if *flagLogLevel == "verbose1" {
		logLevel = log.Verbose1
	} else if *flagLogLevel == "verbose2" {
		logLevel = log.Verbose2
	} else if *flagLogLevel == "verbose3" {
		logLevel = log.Verbose3
	} else {
		fmt.Fprintf(os.Stderr, "Invalid --loglevel, try error, warning, info, verbose1, verbose2 or verbose3")
		os.Exit(1)
	}

	app, err := newNerdlogApp(
		nerdlogAppParams{
			clock:                appClock,
			initialOptionSets:    *flagSet,
			initialQueryData:     initialQueryData,
			connectRightAway:     connectRightAway,
			clipboardInitErr:     clipboard.InitErr,
			logLevel:             logLevel,
			sshConfigPath:        *flagSSHConfig,
			logstreamsConfigPath: *flagLStreamsConfig,
			cmdHistoryFile:       *flagCmdHistoryFile,
			searchHistoryFile:    *flagSearchHistoryFile,
			sshKeys:              *flagSSHKeys,

			noJournalctlAccessWarn: *flagNoJournalctlAccessWarn,
		},
		queryCLHistory,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	fmt.Println("Starting UI ...")
	if err := app.runTViewApp(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	// We end up here when the user quits the UI

	fmt.Println("")
	fmt.Println("Closing connections...")

	app.Close()
	app.Wait()

	fmt.Println("Have a nice day.")
}
