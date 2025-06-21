package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dimonomid/clock"
	"github.com/dimonomid/nerdlog/core/testutils"
	"github.com/dimonomid/nerdlog/log"
	"github.com/juju/errors"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

const coreTestOutputRoot = "/tmp/nerdlog_core_test_output"
const coreTestScenarioYamlFname = "test_scenario.yaml"

type CoreTestScenarioYaml struct {
	Descr string `yaml:"descr"`

	CurrentTime testutils.MyTime `yaml:"current_time"`

	ManagerParams CoreTestScenarioManagerParams `yaml:"manager_params"`
	TestSteps     []CoreTestStep                `yaml:"test_steps"`
}

// CoreTestScenarioManagerParams converts into LStreamsManagerParams.
type CoreTestScenarioManagerParams struct {
	ConfigLogStreams map[string]CoreTestConfigLogStream `yaml:"config_log_streams"`

	InitialLStreams string `yaml:"initial_lstreams"`
	ClientID        string `yaml:"client_id"`
}

// CoreTestConfigLogStream converts to ConfigLogStream (from config.go)
type CoreTestConfigLogStream struct {
	LogFiles testutils.TestCaseLogfiles `yaml:"log_files"`

	Options ConfigLogStreamOptions `yaml:"options"`
}

type CoreTestStep struct {
	// Descr is a human-readable step description
	Descr string `yaml:"descr"`

	// Exactly one of the fields below must be non-nil

	CheckState *CoreTestStepCheckState `yaml:"check_state"`

	// If Query is non-nil, we'll send a query to the LStreamsManager.
	Query *CoreTestStepQuery `yaml:"query"`
}

type CoreTestStepCheckState struct {
	// WantByHostnameAndTransport is a map from the test hostname-and-transport
	// string to the filename with the expected LStreamsManagerState.
	//
	// The hostname-and-transport string looks like this:
	//
	// - "localhost": for localhost, it's just this, since there is only one kind
	//   of transport
	// - "127.0.0.1_ssh-lib": for non-localhost hostname without UseExternalSSH
	// - "127.0.0.1_ssh-bin": for non-localhost hostname with UseExternalSSH
	WantByHostnameAndTransport map[string]string `yaml:"want_by_hostname_and_transport"`
}

type CoreTestStepQuery struct {
	Params CoreTestStepQueryParams `yaml:"params"`

	// Want is a filename (relative to the test scenario dir) with the expected
	// results.
	Want string `yaml:"want"`
}

// CoreTestStepQueryParams converts into QueryLogsParams (from core.go).
type CoreTestStepQueryParams struct {
	MaxNumLines int `yaml:"max_num_lines"`

	From testutils.MyTime `yaml:"from"`
	To   testutils.MyTime `yaml:"to"`

	Pattern string `yaml:"pattern"`

	LoadEarlier bool `yaml:"load_earlier"`

	RefreshIndex bool `yaml:"refresh_index"`
}

func (p *CoreTestStepQueryParams) RealParams() QueryLogsParams {
	return QueryLogsParams{
		MaxNumLines:  p.MaxNumLines,
		From:         p.From.Time,
		To:           p.To.Time,
		Query:        p.Pattern,
		LoadEarlier:  p.LoadEarlier,
		RefreshIndex: p.RefreshIndex,
	}
}

func TestCoreScenarios(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to get caller info")
	}

	// Get directory of the current file
	parentDir := filepath.Dir(filename)
	testScenariosDir := filepath.Join(parentDir, "core_testdata", "test_cases_core")

	repoRoot := filepath.Dir(filepath.Dir(filename))

	if err := os.MkdirAll(coreTestOutputRoot, 0755); err != nil {
		t.Fatalf("unable to create core test output root dir %s: %s", coreTestOutputRoot, err.Error())
	}

	testScenarioDirs, err := testutils.GetTestCaseDirs(testScenariosDir, coreTestScenarioYamlFname)
	if err != nil {
		panic(err)
	}

	for _, testName := range testScenarioDirs {
		t.Run(testName, func(t *testing.T) {
			tsCtx := &coreTestScenarioContext{
				testName:        testName,
				testScenarioDir: filepath.Join(testScenariosDir, testName),
				testOutputDir:   filepath.Join(coreTestOutputRoot, testName),
				repoRoot:        repoRoot,
			}

			if err := runCoreTestScenario(t, tsCtx); err != nil {
				t.Fatalf("running core test scenario %s: %s", testName, err.Error())
			}
		})
	}
}

type coreTestScenarioContext struct {
	testName        string
	testScenarioDir string
	testOutputDir   string
	repoRoot        string
}

func runCoreTestScenario(t *testing.T, tsCtx *coreTestScenarioContext) error {
	testScenarioDescrFname := filepath.Join(tsCtx.testScenarioDir, coreTestScenarioYamlFname)

	if err := os.MkdirAll(tsCtx.testOutputDir, 0755); err != nil {
		return errors.Annotatef(err, "unable to create test output dir %s", tsCtx.testOutputDir)
	}

	data, err := os.ReadFile(testScenarioDescrFname)
	if err != nil {
		return errors.Annotatef(err, "reading yaml test case descriptor %s", testScenarioDescrFname)
	}

	var tc CoreTestScenarioYaml
	if err := yaml.Unmarshal(data, &tc); err != nil {
		return errors.Annotatef(err, "unmarshaling yaml from %s", testScenarioDescrFname)
	}

	if tc.CurrentTime.Time.IsZero() {
		return errors.Annotatef(err, "current_time must not be zero in %s", testScenarioDescrFname)
	}

	clockMock := clock.NewMock()
	clockMock.Set(tc.CurrentTime.Time)

	manTH, err := newLStreamsManagerTestHelper(tc.ManagerParams, tsCtx, clockMock)
	if err != nil {
		return errors.Annotatef(err, "creating LStreamsManagerTestHelper")
	}

	fmt.Println("Waiting connection...")
	manTH.WaitConnected()

	isFirstQuery := true
	for i, step := range tc.TestSteps {
		stepSID := fmt.Sprintf("%.2d_%s", i+1, testutils.Slug(step.Descr))
		stepOutputDir := filepath.Join(tsCtx.testOutputDir, "steps", stepSID)
		if err := os.MkdirAll(stepOutputDir, 0755); err != nil {
			return errors.Annotatef(err, "creating lstream dir %s", stepSID)
		}

		assertArgs := []interface{}{"test case %s", stepSID}

		if checkState := step.CheckState; checkState != nil {
			lsmStateStr := formatLSMState(manTH.GetLSMState())
			err = os.WriteFile(filepath.Join(stepOutputDir, "got_logstreams_manager_state.txt"), []byte(lsmStateStr), 0644)
			if err != nil {
				return errors.Annotatef(err, "test step #%d: writing lsm state", i)
			}

			hostnameAndTransportKey := manTH.getHostnameAndTransportKey()
			wantFilename := checkState.WantByHostnameAndTransport[hostnameAndTransportKey]

			if wantFilename == "" {
				return errors.Errorf("test step #%d: no WantByHostnameAndTransport for %q", i, hostnameAndTransportKey)
			} else {
				err = os.WriteFile(filepath.Join(stepOutputDir, "want_lsm_state_filename.txt"), []byte(wantFilename), 0644)
				if err != nil {
					return errors.Annotatef(err, "test step #%d: writing want_lsm_state_filename.txt", i)
				}

				wantLSMStateFilenameFull := filepath.Join(tsCtx.testScenarioDir, wantFilename)
				wantLSMState, err := os.ReadFile(wantLSMStateFilenameFull)
				if err != nil {
					return errors.Annotatef(err, "test step #%d: reading wanted log resp %s", i, wantLSMStateFilenameFull)
				}

				assert.Equal(t, string(wantLSMState), lsmStateStr, assertArgs...)
			}
		} else if query := step.Query; query != nil {
			// For reproducibility of the exact same debug output, refresh the index
			// during the first query in a scenario.
			if isFirstQuery {
				query.Params.RefreshIndex = true
				isFirstQuery = false
			}

			logResp, err := manTH.QueryLogs(query.Params)
			if err != nil {
				return errors.Annotatef(err, "test step #%d: querying logs %+v", i, query.Params)
			}

			logRespStr := formatLogResp(logResp)
			err = os.WriteFile(filepath.Join(stepOutputDir, "got_log_resp.txt"), []byte(logRespStr), 0644)
			if err != nil {
				return errors.Annotatef(err, "test step #%d: writing log resp", i)
			}

			err = os.WriteFile(filepath.Join(stepOutputDir, "want_log_resp_filename.txt"), []byte(query.Want), 0644)
			if err != nil {
				return errors.Annotatef(err, "test step #%d: writing want_log_resp_filename.txt", i)
			}

			wantLogRespFilenameFull := filepath.Join(tsCtx.testScenarioDir, query.Want)
			wantLogResp, err := os.ReadFile(wantLogRespFilenameFull)
			if err != nil {
				return errors.Annotatef(err, "test step #%d: reading wanted log resp %s", i, wantLogRespFilenameFull)
			}

			assert.Equal(t, string(wantLogResp), logRespStr, assertArgs...)
		}
	}

	fmt.Println("Closing LStreamsManager...")
	if err := manTH.CloseAndWait(); err != nil {
		return errors.Trace(err)
	}

	return nil
}

type LStreamsManagerTestHelper struct {
	manager   *LStreamsManager
	updatesCh chan LStreamsManagerUpdate
	clock     *clock.Mock

	state    LStreamsManagerTestHelperState
	stateMtx sync.Mutex
}

type LStreamsManagerTestHelperState struct {
	lsmState        *LStreamsManagerState
	pendingLogResps []*LogRespTotal
}

func newLStreamsManagerTestHelper(
	params CoreTestScenarioManagerParams,
	tsCtx *coreTestScenarioContext,
	clockMock *clock.Mock,
) (*LStreamsManagerTestHelper, error) {
	updatesCh := make(chan LStreamsManagerUpdate, 100)

	cfgLogStreams := make(ConfigLogStreams, len(params.ConfigLogStreams))
	for lstreamName, testCfg := range params.ConfigLogStreams {
		resolved, err := testutils.ResolveLogfiles(tsCtx.testScenarioDir, &testCfg.LogFiles)
		if err != nil {
			return nil, errors.Annotatef(err, "resolving logfiles")
		}

		testOutputLstreamDir := filepath.Join(tsCtx.testOutputDir, "lstreams", lstreamName)
		if err := os.MkdirAll(testOutputLstreamDir, 0755); err != nil {
			return nil, errors.Annotatef(err, "creating lstream dir %s", lstreamName)
		}

		provisioned, err := testutils.ProvisionLogFiles(
			resolved,
			testOutputLstreamDir,
			tsCtx.repoRoot,
		)
		if err != nil {
			return nil, errors.Annotatef(err, "provisioning logfiles")
		}

		options := testCfg.Options
		for _, envVar := range provisioned.ExtraEnv {
			options.ShellInit = append(options.ShellInit, fmt.Sprintf("export %s", envVar))
		}

		cfgLogStreams[lstreamName] = ConfigLogStream{
			Hostname: getCoreTestHostname(),
			LogFiles: []string{
				provisioned.LogfileLast,
				provisioned.LogfilePrev,
			},
			Options: options,
		}
	}

	transportSpec := os.Getenv("NERDLOG_CORE_TEST_TRANSPORT")
	if transportSpec == "" {
		transportSpec = "ssh-lib"
	}

	transportMode, err := ParseTransportMode(transportSpec)
	if err != nil {
		return nil, errors.Trace(err)
	}

	manParams := LStreamsManagerParams{
		ConfigLogStreams: cfgLogStreams,
		Logger:           log.NewLogger(log.Verbose1).WithStdout(true),
		InitialLStreams:  params.InitialLStreams,
		ClientID:         params.ClientID,
		UpdatesCh:        updatesCh,
		Clock:            clockMock,

		InitialDefaultTransportMode: transportMode,
	}

	fmt.Println("Creating LStreamsManager...")
	manager := NewLStreamsManager(manParams)

	manTH := &LStreamsManagerTestHelper{
		manager:   manager,
		updatesCh: updatesCh,
		clock:     clockMock,
	}

	go manTH.run()

	return manTH, nil
}

// getCoreTestHostname finds out the hostname: normally we use just
// `localhost`, which means we'll use ShellTransportLocal, but it can be
// overridden with the NERDLOG_CORE_TEST_HOSTNAME env var. Keep in mind that
// only `localhost` bypasses ssh; so e.g. "127.0.0.1" will use
// ShellTransportSSH, and we can take advantage of that to cover the ssh
// transport in tests. Obviously, for that the ssh server needs to be running
// locally.
func getCoreTestHostname() string {
	hostname := os.Getenv("NERDLOG_CORE_TEST_HOSTNAME")
	if hostname == "" {
		hostname = "localhost"
	}

	return hostname
}

func (th *LStreamsManagerTestHelper) getHostnameAndTransportKey() string {
	testHostname := getCoreTestHostname()
	if testHostname == "localhost" {
		return testHostname
	}

	transportStr := th.manager.defaultTransportMode.String()

	return fmt.Sprintf("%s_%s", testHostname, transportStr)
}

func (th *LStreamsManagerTestHelper) run() {
	for upd := range th.updatesCh {
		th.applyUpdate(upd)
	}
}

func (th *LStreamsManagerTestHelper) applyUpdate(upd LStreamsManagerUpdate) {
	//d, _ := json.Marshal(upd)
	//fmt.Printf("    UPD: %s\n", string(d))

	th.stateMtx.Lock()
	defer th.stateMtx.Unlock()

	if upd.State != nil {
		th.state.lsmState = upd.State
	} else if upd.LogResp != nil {
		th.state.pendingLogResps = append(th.state.pendingLogResps, upd.LogResp)
	}
}

func (th *LStreamsManagerTestHelper) isConnected() bool {
	th.stateMtx.Lock()
	defer th.stateMtx.Unlock()

	return th.state.lsmState != nil && th.state.lsmState.Connected
}

func (th *LStreamsManagerTestHelper) nextLogResp() *LogRespTotal {
	th.stateMtx.Lock()
	defer th.stateMtx.Unlock()

	if len(th.state.pendingLogResps) == 0 {
		return nil
	}

	ret := th.state.pendingLogResps[0]
	th.state.pendingLogResps = th.state.pendingLogResps[1:]

	return ret
}

func (th *LStreamsManagerTestHelper) WaitConnected() {
	for {
		if th.isConnected() {
			return
		}

		// TODO: We could implement subscribing to state updates, but for now just polling.
		time.Sleep(100 * time.Millisecond)
	}
}

func (th *LStreamsManagerTestHelper) QueryLogs(params CoreTestStepQueryParams) (*LogRespTotal, error) {
	// Sanity check that there is no existing pending log resp
	existing := th.nextLogResp()
	if existing != nil {
		return existing, errors.Errorf("there was existing pending log resp")
	}

	th.manager.QueryLogs(params.RealParams())

	return th.WaitNextLogResp()
}

func (th *LStreamsManagerTestHelper) GetLSMState() *LStreamsManagerState {
	return th.state.lsmState
}

func (th *LStreamsManagerTestHelper) WaitNextLogResp() (*LogRespTotal, error) {
	start := time.Now()

	for {
		ret := th.nextLogResp()
		if ret != nil {
			return ret, nil
		}

		if time.Since(start) > 5*time.Second {
			return nil, errors.Errorf("timed out waiting for log resp")
		}

		// TODO: We could implement subscribing to state updates, but for now just polling.
		time.Sleep(100 * time.Millisecond)
	}
}

func (th *LStreamsManagerTestHelper) CloseAndWait() error {
	th.manager.Close()

	closedCh := make(chan struct{})
	go func() {
		th.manager.Wait()
		close(closedCh)
	}()

	select {
	case <-closedCh:
		// All good
		return nil
	case <-time.After(5 * time.Second):
		return errors.Errorf("timed out waiting for LStreamsManager to close")
	}
}

func formatLogResp(logResp *LogRespTotal) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("NumMsgsTotal: %v\n", logResp.NumMsgsTotal))
	sb.WriteString(fmt.Sprintf("LoadedEarlier: %v\n", logResp.LoadedEarlier))
	sb.WriteString(fmt.Sprintf("Num errors: %v\n", len(logResp.Errs)))
	for _, err := range logResp.Errs {
		sb.WriteString(fmt.Sprintf("- %s", err.Error()))
	}

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Num MinuteStats: %v\n", len(logResp.MinuteStats)))
	printMinuteStats(&sb, logResp.MinuteStats)

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Num Logs: %v\n", len(logResp.Logs)))
	printLogs(&sb, logResp.Logs)

	sb.WriteString("\n")
	debugInfoData, _ := json.MarshalIndent(logResp.DebugInfo, "", "  ")
	sb.WriteString(fmt.Sprintf("DebugInfo:\n%s", debugInfoData))

	return sb.String()
}

func formatLSMState(lsmState *LStreamsManagerState) string {
	data, _ := json.MarshalIndent(lsmState, "", "  ")
	str := string(data)

	// We also need to replace the OS username in the payload with a static
	// placeholder, since it can be different.
	str = maskOSUser(str)

	return str
}

// maskOSUser returns a string with all occurrences of the current OS user
// replaced with "__TEST_OS_USER__".
func maskOSUser(s string) string {
	u, err := user.Current()
	if err != nil {
		// Can't determine the user, return the input unchanged
		return s
	}

	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(u.Username) + `\b`)
	return re.ReplaceAllString(s, "__TEST_OS_USER__")
}

func printMinuteStats(w io.Writer, stats map[int64]MinuteStatsItem) {
	// Extract and sort timestamps for consistent output
	timestamps := make([]int64, 0, len(stats))
	for ts := range stats {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})

	// Print each item
	for _, ts := range timestamps {
		t := time.Unix(ts, 0).UTC()
		formatted := t.Format("2006-01-02-15-04")
		fmt.Fprintf(w, "- %s: %d\n", formatted, stats[ts].NumMsgs)
	}
}

func printLogs(w io.Writer, logs []LogMsg) {
	for _, msg := range logs {
		fmt.Fprintf(w, "- %s", msg.Time.Format("2006-01-02T15:04:05.000000000Z07:00"))
		if msg.DecreasedTimestamp {
			fmt.Fprintf(w, ",T")
		} else {
			fmt.Fprintf(w, ",F")
		}

		fmt.Fprintf(w, ",%s", msg.LogFilename)
		fmt.Fprintf(w, ",%.6d", msg.LogLinenumber)
		fmt.Fprintf(w, ",%.6d", msg.CombinedLinenumber)
		fmt.Fprintf(w, ",%s", logLevelToStr(msg.Level))
		fmt.Fprintf(w, ",%s", msg.Msg)

		ctxData, _ := json.Marshal(msg.Context)
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "  context: %s", string(ctxData))

		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "  orig: %s", msg.OrigLine)

		fmt.Fprintf(w, "\n")
	}
}

func logLevelToStr(logLevel LogLevel) string {
	switch logLevel {
	case LogLevelUnknown:
		return "----"
	case LogLevelDebug:
		return "debg"
	case LogLevelInfo:
		return "info"
	case LogLevelWarn:
		return "warn"
	case LogLevelError:
		return "erro"
	default:
		return "????"
	}
}
