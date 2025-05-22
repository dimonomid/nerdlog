package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

func isGawkAvailable() bool {
	_, err := exec.LookPath("gawk")
	return err == nil
}

func TestMain(m *testing.M) {
	if !isGawkAvailable() {
		fmt.Println("gawk not found, skipping core tests that require it")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

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
	LogFiles TestCaseLogfiles `yaml:"log_files"`

	Options ConfigLogStreamOptions `yaml:"options"`
}

type CoreTestStep struct {
	// Descr is a human-readable step description
	Descr string `yaml:"descr"`

	// Exactly one of the fields below must be non-nil

	// If Query is non-nil, we'll send a query to the LStreamsManager.
	Query *CoreTestStepQuery `yaml:"query"`
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

	testScenarioDirs, err := getTestCaseDirs(testScenariosDir, coreTestScenarioYamlFname)
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

	for i, step := range tc.TestSteps {
		stepSID := fmt.Sprintf("%.2d_%s", i+1, testutils.Slug(step.Descr))
		stepOutputDir := filepath.Join(tsCtx.testOutputDir, "steps", stepSID)
		if err := os.MkdirAll(stepOutputDir, 0755); err != nil {
			return errors.Annotatef(err, "creating lstream dir %s", stepSID)
		}

		assertArgs := []interface{}{"test case %s", stepSID}

		if query := step.Query; query != nil {
			// For reproducibility of the exact same debug output, refresh the index
			// during the first query in a scenario.
			if i == 0 {
				query.Params.RefreshIndex = true
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
	manTH.CloseAndWait()

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
	connected       bool
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
		resolved, err := resolveLogfiles(tsCtx.testScenarioDir, &testCfg.LogFiles)
		if err != nil {
			return nil, errors.Annotatef(err, "resolving logfiles")
		}

		testOutputLstreamDir := filepath.Join(tsCtx.testOutputDir, "lstreams", lstreamName)
		if err := os.MkdirAll(testOutputLstreamDir, 0755); err != nil {
			return nil, errors.Annotatef(err, "creating lstream dir %s", lstreamName)
		}

		provisioned, err := provisionLogFiles(
			resolved,
			testOutputLstreamDir,
			tsCtx.repoRoot,
		)
		if err != nil {
			return nil, errors.Annotatef(err, "provisioning logfiles")
		}

		options := testCfg.Options
		for _, envVar := range provisioned.extraEnv {
			options.ShellInit = append(options.ShellInit, fmt.Sprintf("export %s", envVar))
		}

		cfgLogStreams[lstreamName] = ConfigLogStream{
			Hostname: "localhost",
			LogFiles: []string{
				provisioned.logfileLast,
				provisioned.logfilePrev,
			},
			Options: options,
		}
	}

	manParams := LStreamsManagerParams{
		ConfigLogStreams: cfgLogStreams,
		Logger:           log.NewLogger(log.Verbose1).WithStdout(true),
		InitialLStreams:  params.InitialLStreams,
		ClientID:         params.ClientID,
		UpdatesCh:        updatesCh,
		Clock:            clockMock,
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
		th.state.connected = upd.State.Connected
	} else if upd.LogResp != nil {
		th.state.pendingLogResps = append(th.state.pendingLogResps, upd.LogResp)
	}
}

func (th *LStreamsManagerTestHelper) isConnected() bool {
	th.stateMtx.Lock()
	defer th.stateMtx.Unlock()

	return th.state.connected
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

func (th *LStreamsManagerTestHelper) CloseAndWait() {
	th.manager.Close()
	th.manager.Wait()
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
