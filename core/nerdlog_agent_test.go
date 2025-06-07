package core

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dimonomid/nerdlog/core/testutils"
	"github.com/dimonomid/nerdlog/util/sysloggen"
	"github.com/juju/errors"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

const agentTestOutputRoot = "/tmp/nerdlog_agent_test_output"
const agentTestCaseYamlFname = "test_case.yaml"

type AgentTestCaseYaml struct {
	// If Disabled is true, the test case is skipped.
	Disabled bool `yaml:"disabled"`

	Descr    string                     `yaml:"descr"`
	Logfiles testutils.TestCaseLogfiles `yaml:"logfiles"`

	// CurYear and CurMonth specify today's date. If not specified, 1970-01 will
	// be used. This matters for inferring the log's year (because traditional
	// syslog timestamp format doesn't include year).
	CurYear  int `yaml:"cur_year"`
	CurMonth int `yaml:"cur_month"`

	// Env can contain extra environment variables to set, in the format
	// VARIABLE=VALUE. It'll be passed to cmd.Env directly.
	Env []string `yaml:"env"`

	Args []string `yaml:"args"`
}

func TestNerdlogAgent(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to get caller info")
	}

	// Get directory of the current file
	parentDir := filepath.Dir(filename)
	testCasesDir := filepath.Join(parentDir, "core_testdata", "test_cases_agent")
	nerdlogAgentShFname := filepath.Join(parentDir, "nerdlog_agent.sh")

	repoRoot := filepath.Dir(filepath.Dir(filename))

	if err := os.MkdirAll(agentTestOutputRoot, 0755); err != nil {
		t.Fatalf("unable to create agent test output root dir %s: %s", agentTestOutputRoot, err.Error())
	}

	testCaseDirs, err := testutils.GetTestCaseDirs(testCasesDir, agentTestCaseYamlFname)
	if err != nil {
		panic(err)
	}

	for _, testCaseDir := range testCaseDirs {
		t.Run(testCaseDir, func(t *testing.T) {
			if err := runAgentTestCase(t, nerdlogAgentShFname, testCasesDir, repoRoot, testCaseDir); err != nil {
				t.Fatalf("running agent test case %s: %s", testCaseDir, err.Error())
			}
		})
	}
}

func runAgentTestCase(t *testing.T, nerdlogAgentShFname, testCasesDir, repoRoot, testName string) error {
	testCaseDir := filepath.Join(testCasesDir, testName)
	testCaseDescrFname := filepath.Join(testCaseDir, agentTestCaseYamlFname)

	testOutputDir := filepath.Join(agentTestOutputRoot, testName)
	if err := os.MkdirAll(testOutputDir, 0755); err != nil {
		return errors.Annotatef(err, "unable to create test output dir %s", testOutputDir)
	}

	data, err := os.ReadFile(testCaseDescrFname)
	if err != nil {
		return errors.Annotatef(err, "reading yaml test case descriptor %s", testCaseDescrFname)
	}

	var tc AgentTestCaseYaml
	if err := yaml.Unmarshal(data, &tc); err != nil {
		return errors.Annotatef(err, "unmarshaling yaml from %s", testCaseDescrFname)
	}

	if tc.Disabled {
		fmt.Printf("WARNING: Skipping %s since it's disabled\n", testName)
		return nil
	}

	resolved, err := testutils.ResolveLogfiles(testCaseDir, &tc.Logfiles)
	if err != nil {
		return errors.Annotatef(err, "resolving logfiles")
	}

	provisioned, err := testutils.ProvisionLogFiles(resolved, testOutputDir, repoRoot)
	if err != nil {
		return errors.Annotatef(err, "provisioning logfiles")
	}

	indexFname := filepath.Join(testOutputDir, "nerdlog_agent_index")

	os.Remove(indexFname)

	cmdArgs := []string{
		nerdlogAgentShFname,
		"query",
		"--logfile-last", provisioned.LogfileLast,
		"--logfile-prev", provisioned.LogfilePrev,
		"--index-file", indexFname,
	}

	if provisioned.LogfileLast == "journalctl" {
		// Specify time format (normally LStreamClient autodetects the time format
		// and provides these).
		cmdArgs = append(
			cmdArgs,
			"--awktime-month", "substr($0, 6, 2)",
			"--awktime-year", "substr($0, 1, 4)",
			"--awktime-day", "substr($0, 9, 2)",
			"--awktime-hhmm", "substr($0, 12, 5)",
		)
	}

	cmdArgs = append(cmdArgs, tc.Args...)

	// Do the full run, with the provided initial index (which in most cases
	// means, without any index)
	if err := runNerdlogAgent(t, &tc, cmdArgs, testCaseDir, provisioned.ExtraEnv, testName, testNerdlogAgentParams{
		checkStderr: true,
	}); err != nil {
		return errors.Trace(err)
	}

	// For journalctl tests, there is no index, and therefore nothing else to do.
	if provisioned.LogfileLast == "journalctl" {
		return nil
	}

	// For log files tests, we rerun the test multiple times after removing some
	// latest lines from the index, expecting it to index up and to produce the same
	// result.

	// If asked to skip these repetitions with indexing up, we're done.
	//
	// There are at least two valid reasons to skip:
	//
	// - When we're running "make update-test-expectations", because for that we
	//   need to capture stderr after the main test (without repetitions), since
	//   stderr will be different otherwise
	// - To run tests much faster (these repetitions are the slowest part of tests)
	if os.Getenv("NERDLOG_AGENT_TEST_SKIP_INDEX_UP") != "" {
		return nil
	}

	// indexReduceStep specifies how many lines we remove from the index at every
	// step here. For most tests, it's 25.
	indexReduceStep := 25

	// For the tests of handling decreased timestamps, reduce the index by a single
	// line, to make sure that some corner case doesn't slip through.
	if strings.Contains(testName, "decreased") {
		indexReduceStep = 1
	}

	// Backup the resulting fully-built index
	indexFullFname := filepath.Join(testOutputDir, "nerdlog_agent_index_full")
	if err := testutils.CopyFile(indexFname, indexFullFname); err != nil {
		return errors.Annotatef(err, "backing up index as full index: from %s to %s", indexFname, indexFullFname)
	}

	// Now, keep running the same query with smaller index: on every iteration,
	// we'll remove one more line from the index end, and expect the same stdout
	// (not stderr though, this one will be different).

	numLines, err := getNumLines(indexFullFname)
	if err != nil {
		return errors.Annotatef(err, "getting numer of lines in %s", indexFullFname)
	}

	// We can only remove the "idx" lines from the index.
	minLineno, err := getLastNonMatchingLine(indexFullFname, "idx")
	if err != nil {
		return errors.Annotatef(err, "getLastNonMatchingLine")
	}

	// minLineno points to the line containing the last non-"idx" entry, but
	// we actually need at least one "idx" entry in the index file for it to work,
	// so we increment it.
	minLineno += 1

	for keepLines := numLines - 1; ; keepLines -= indexReduceStep {
		// If we step too much below the min, use the min (and we'll break below).
		if keepLines < minLineno {
			keepLines = minLineno
		}

		_, err := copyUpToNumLines(indexFullFname, indexFname, keepLines)
		if err != nil {
			return errors.Annotatef(
				err, "copying up to %d lines from %s to %s",
				keepLines, indexFullFname, indexFname,
			)
		}

		t.Run(fmt.Sprintf("keep_%d_lines", keepLines), func(t *testing.T) {
			if err := runNerdlogAgent(t, &tc, cmdArgs, testCaseDir, provisioned.ExtraEnv, testName, testNerdlogAgentParams{
				// When changing the index, stderr would change too.
				checkStderr: false,
			}); err != nil {
				t.Fatalf("error: %s", err.Error())
			}
		})

		if keepLines <= minLineno {
			break
		}
	}

	// TODO: the stats lines in stdout (these starting from "s:") are printed in
	// arbitrary order because they come from a hashmap, so simply comparing the
	// output is a bad idea. Gotta do some post-processing, like sorting these "s:"
	// lines, before comparing them.

	return nil
}

type testNerdlogAgentParams struct {
	checkStderr bool
}

func runNerdlogAgent(
	t *testing.T, tc *AgentTestCaseYaml, bashArgs []string, testCaseDir string,
	extraEnv []string,
	testName string,
	params testNerdlogAgentParams,
) error {
	assertArgs := []interface{}{"test case %s", testName}

	testOutputDir := filepath.Join(agentTestOutputRoot, testName)

	stdoutFname := filepath.Join(testOutputDir, "nerdlog_agent_stdout")
	stderrFname := filepath.Join(testOutputDir, "nerdlog_agent_stderr")
	os.Remove(stdoutFname)
	os.Remove(stderrFname)

	stdoutFile, err := os.Create(stdoutFname)
	defer stdoutFile.Close()

	stderrFile, err := os.Create(stderrFname)
	defer stderrFile.Close()

	cmd := exec.Command("/usr/bin/env", append([]string{"bash"}, bashArgs...)...)

	curYear := tc.CurYear
	if curYear == 0 {
		curYear = 1970
	}

	curMonth := tc.CurMonth
	if curMonth == 0 {
		curMonth = 1
	}

	agentEnv := []string{
		"TZ=UTC",
		fmt.Sprintf("CUR_YEAR=%d", curYear),
		fmt.Sprintf("CUR_MONTH=%d", curMonth),
	}

	agentEnv = append(agentEnv, tc.Env...)
	agentEnv = append(agentEnv, extraEnv...)

	cmd.Env = append(
		os.Environ(),
		agentEnv...,
	)
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	fmt.Printf("Running %+v\n", bashArgs)
	if err := cmd.Run(); err != nil {
		return errors.Annotatef(err, "running nerdlog query command %+v", bashArgs)
	}

	wantStdout, err := os.ReadFile(filepath.Join(testCaseDir, "want_stdout"))
	if err != nil {
		return errors.Annotatef(err, "reading want_stdout")
	}

	wantStderr, err := os.ReadFile(filepath.Join(testCaseDir, "want_stderr"))
	if err != nil {
		return errors.Annotatef(err, "reading want_stderr")
	}

	if err := sortStatBucketLinesInFile(stdoutFname); err != nil {
		return errors.Annotatef(err, "transforming stdout %s using sortStatBucketLinesInFile", stdoutFname)
	}

	gotStdout, err := os.ReadFile(stdoutFname)
	if err != nil {
		return errors.Annotatef(err, "reading %s", stdoutFname)
	}

	gotStderr, err := os.ReadFile(stderrFname)
	if err != nil {
		return errors.Annotatef(err, "reading %s", stderrFname)
	}

	assert.Equal(t, string(wantStdout), string(gotStdout), assertArgs...)

	if params.checkStderr {
		assert.Equal(t, string(wantStderr), string(gotStderr), assertArgs...)
	}

	return nil
}

// sortStatBucketLines takes a string representing stdout output from the
// nerdlog agent script, like this:
//
//	logfile:/tmp/nerdlog_agent_test_output/all_existing_logs/01_from_is_set_to_is_set/logfile.1:0
//	logfile:/tmp/nerdlog_agent_test_output/all_existing_logs/01_from_is_set_to_is_set/logfile:19
//	s:Mar 10 10:20,2
//	s:Mar 10 09:39,1
//	s:Mar 10 09:00,1
//	s:Mar 10 09:59,1
//	s:Mar 10 10:32,2
//	m:34:Mar 10 10:51:01 myhost user[3758]: <crit> System running low on resources
//	m:35:Mar 10 10:57:37 myhost news[5185]: <alert> Insufficient privileges
//	exit_code:0
//
// And returns the same string, but all the lines starting from "s:" being sorted
// lexicographically. This is just for better testability.
func sortStatBucketLines(nerdlogStdout []byte) []byte {
	scanner := bufio.NewScanner(bytes.NewReader(nerdlogStdout))
	var out bytes.Buffer
	var statLines []string

	flushStatLines := func() {
		if len(statLines) > 0 {
			sort.Strings(statLines)
			for _, line := range statLines {
				out.WriteString(line)
				out.WriteByte('\n')
			}
			statLines = nil
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 2 && line[:2] == "s:" {
			statLines = append(statLines, line)
		} else {
			flushStatLines()
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}

	flushStatLines() // in case the input ends with s: lines

	return out.Bytes()
}

// sortStatBucketLinesInFile reads all file contents using the given filename,
// transforms it using sortStatBucketLines, and writes back to the same file.
func sortStatBucketLinesInFile(fname string) error {
	data, err := os.ReadFile(fname)
	if err != nil {
		return errors.Annotatef(err, "reading in sortStatBucketLinesInFile")
	}

	sorted := sortStatBucketLines(data)

	err = os.WriteFile(fname, sorted, 0644)
	if err != nil {
		return errors.Annotatef(err, "writing in sortStatBucketLinesInFile")
	}

	return nil
}

func runNerdlogAgentForBenchmark(
	bashArgs []string,
) error {
	cmd := exec.Command("/usr/bin/env", append([]string{"bash"}, bashArgs...)...)

	cmd.Env = append(os.Environ(), "TZ=UTC")

	if err := cmd.Run(); err != nil {
		return errors.Annotatef(err, "running nerdlog query command %+v", bashArgs)
	}

	return nil
}

// getNumLines returns the number of lines in the given file.
func getNumLines(fname string) (int, error) {
	file, err := os.Open(fname)
	if err != nil {
		return 0, errors.Annotatef(err, "failed to open file %q", fname)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}

	if err := scanner.Err(); err != nil {
		return 0, errors.Annotate(err, "error while scanning file")
	}

	return lineCount, nil
}

// getLastNonMatchingLine returns the number of the last line which does not start
// with the given string. Line numbers are 1-based.
func getLastNonMatchingLine(fname string, prefix string) (int, error) {
	file, err := os.Open(fname)
	if err != nil {
		return 0, errors.Annotatef(err, "failed to open file: %s", fname)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	lastNonMatchingLine := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if !strings.HasPrefix(line, prefix) {
			lastNonMatchingLine = lineNumber
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, errors.Annotate(err, "error scanning file")
	}

	return lastNonMatchingLine, nil
}

// copyUpToNumLines copies srcPath as destPath, but only the first maxNumLines.
// It returns the last line.
func copyUpToNumLines(srcPath, destPath string, maxNumLines int) (string, error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return "", errors.Annotatef(err, "failed to open source file: %s", srcPath)
	}
	defer srcFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return "", errors.Annotatef(err, "failed to create destination file: %s", destPath)
	}
	defer destFile.Close()

	scanner := bufio.NewScanner(srcFile)
	writer := bufio.NewWriter(destFile)
	defer writer.Flush()

	var lastLine string
	lineCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		if lineCount < maxNumLines {
			_, err := writer.WriteString(line + "\n")
			if err != nil {
				return "", errors.Annotate(err, "failed to write line to destination file")
			}
		}
		lastLine = line
		lineCount++
		if lineCount >= maxNumLines {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", errors.Annotate(err, "error while scanning source file")
	}

	return lastLine, nil
}

func BenchmarkNerdlogAgentSmallLogNoIndex(b *testing.B) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		b.Fatal("unable to get caller info")
	}

	parentDir := filepath.Dir(filename)
	logfilesDir := filepath.Join(parentDir, "core_testdata", "input_logfiles", "small_mar")
	nerdlogAgentShFname := filepath.Join(parentDir, "nerdlog_agent.sh")

	indexFname := filepath.Join(agentTestOutputRoot, "bench1_index")

	cmdArgs := append(
		[]string{
			nerdlogAgentShFname,
			"query",
			"--logfile-last", filepath.Join(logfilesDir, "syslog"),
			"--logfile-prev", filepath.Join(logfilesDir, "syslog.1"),
			"--index-file", indexFname,
			"--max-num-lines", "100",
			"--from", "2025-03-12-10:00",
		},
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		os.Remove(indexFname)
		if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
			b.Fatalf("runNerdlogAgentForBenchmark failed: %s", err)
		}
	}
}

func BenchmarkNerdlogAgentSmallLogCompleteIndex(b *testing.B) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		b.Fatal("unable to get caller info")
	}

	parentDir := filepath.Dir(filename)
	logfilesDir := filepath.Join(parentDir, "core_testdata", "input_logfiles", "small_mar")
	nerdlogAgentShFname := filepath.Join(parentDir, "nerdlog_agent.sh")

	indexFname := filepath.Join(agentTestOutputRoot, "bench1_index")

	cmdArgs := append(
		[]string{
			nerdlogAgentShFname,
			"query",
			"--logfile-last", filepath.Join(logfilesDir, "syslog"),
			"--logfile-prev", filepath.Join(logfilesDir, "syslog.1"),
			"--index-file", indexFname,
			"--max-num-lines", "100",
			"--from", "2025-03-12-10:00",
		},
	)

	// Build the index
	os.Remove(indexFname)
	if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
		b.Fatalf("initial runNerdlogAgentForBenchmark failed: %s", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
			b.Fatalf("runNerdlogAgentForBenchmark failed: %s", err)
		}
	}
}

func BenchmarkNerdlogAgentLargeLogSmallPortionNoIndex(b *testing.B) {
	if err := generateLogfilesLarge(); err != nil {
		b.Fatalf("failed to generate log files: %s", err.Error())
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		b.Fatal("unable to get caller info")
	}

	parentDir := filepath.Dir(filename)
	nerdlogAgentShFname := filepath.Join(parentDir, "nerdlog_agent.sh")

	indexFname := filepath.Join(agentTestOutputRoot, "bench_large_index")

	cmdArgs := append(
		[]string{
			nerdlogAgentShFname,
			"query",
			"--logfile-last", "/tmp/nerdlog_agent_test_output/randomlog_large",
			"--logfile-prev", "/tmp/nerdlog_agent_test_output/randomlog_large.1",
			"--index-file", indexFname,
			"--max-num-lines", "100",
			"--from", "2025-03-11-00:00",
		},
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		os.Remove(indexFname)
		if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
			b.Fatalf("runNerdlogAgentForBenchmark failed: %s", err)
		}
	}
}

func BenchmarkNerdlogAgentLargeLogSmallPortionCompleteIndex(b *testing.B) {
	if err := generateLogfilesLarge(); err != nil {
		b.Fatalf("failed to generate log files: %s", err.Error())
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		b.Fatal("unable to get caller info")
	}

	parentDir := filepath.Dir(filename)
	nerdlogAgentShFname := filepath.Join(parentDir, "nerdlog_agent.sh")

	indexFname := filepath.Join(agentTestOutputRoot, "bench_large_index")

	cmdArgs := append(
		[]string{
			nerdlogAgentShFname,
			"query",
			"--logfile-last", "/tmp/nerdlog_agent_test_output/randomlog_large",
			"--logfile-prev", "/tmp/nerdlog_agent_test_output/randomlog_large.1",
			"--index-file", indexFname,
			"--max-num-lines", "100",
			"--from", "2025-03-11-00:00",
		},
	)

	// Build the index
	os.Remove(indexFname)
	if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
		b.Fatalf("initial runNerdlogAgentForBenchmark failed: %s", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
			b.Fatalf("runNerdlogAgentForBenchmark failed: %s", err)
		}
	}
}

func BenchmarkNerdlogAgentLargeLogTinyPortionCompleteIndex(b *testing.B) {
	if err := generateLogfilesLarge(); err != nil {
		b.Fatalf("failed to generate log files: %s", err.Error())
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		b.Fatal("unable to get caller info")
	}

	parentDir := filepath.Dir(filename)
	nerdlogAgentShFname := filepath.Join(parentDir, "nerdlog_agent.sh")

	indexFname := filepath.Join(agentTestOutputRoot, "bench_large_index")

	cmdArgs := append(
		[]string{
			nerdlogAgentShFname,
			"query",
			"--logfile-last", "/tmp/nerdlog_agent_test_output/randomlog_large",
			"--logfile-prev", "/tmp/nerdlog_agent_test_output/randomlog_large.1",
			"--index-file", indexFname,
			"--max-num-lines", "100",
			"--from", "2025-03-11-01:30",
		},
	)

	// Build the index
	os.Remove(indexFname)
	if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
		b.Fatalf("initial runNerdlogAgentForBenchmark failed: %s", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
			b.Fatalf("runNerdlogAgentForBenchmark failed: %s", err)
		}
	}
}

func BenchmarkNerdlogAgentHugeLogOneHourCompleteIndex(b *testing.B) {
	if err := generateLogfilesHuge(); err != nil {
		b.Fatalf("failed to generate log files: %s", err.Error())
	}

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		b.Fatal("unable to get caller info")
	}

	parentDir := filepath.Dir(filename)
	nerdlogAgentShFname := filepath.Join(parentDir, "nerdlog_agent.sh")

	indexFname := filepath.Join(agentTestOutputRoot, "bench_huge_index")

	cmdArgs := append(
		[]string{
			nerdlogAgentShFname,
			"query",
			"--logfile-last", "/tmp/nerdlog_agent_test_output/randomlog_huge",
			"--logfile-prev", "/tmp/nerdlog_agent_test_output/randomlog_huge.1",
			"--index-file", indexFname,
			"--max-num-lines", "100",
			"--from", "2025-03-11-12:30",
		},
	)

	// Build the index
	os.Remove(indexFname)
	if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
		b.Fatalf("initial runNerdlogAgentForBenchmark failed: %s", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := runNerdlogAgentForBenchmark(cmdArgs); err != nil {
			b.Fatalf("runNerdlogAgentForBenchmark failed: %s", err)
		}
	}
}

func generateLogfilesLarge() error {
	t, err := time.Parse(time.RFC3339, "2025-03-09T06:00:00Z")
	if err != nil {
		return errors.Trace(err)
	}

	t2, err := time.Parse(time.RFC3339, "2025-03-10T06:00:00Z")
	if err != nil {
		return errors.Trace(err)
	}

	err = sysloggen.GenerateSyslog(sysloggen.Params{
		StartTime:     t,
		SecondLogTime: t2,

		LogBasename: "/tmp/nerdlog_agent_test_output/randomlog_large",

		NumLogs:    4000000,
		MinDelayMS: 0,
		MaxDelayMS: 80,

		RandomSeed: 123,

		SkipIfPrevLogSizeIs: 143841612,
		SkipIfLastLogSizeIs: 122432250,
	})
	if err != nil {
		return errors.Trace(err)
	}

	return nil
}

func generateLogfilesHuge() error {
	t, err := time.Parse(time.RFC3339, "2025-03-09T06:00:00Z")
	if err != nil {
		return errors.Trace(err)
	}

	t2, err := time.Parse(time.RFC3339, "2025-03-09T09:00:00Z")
	if err != nil {
		return errors.Trace(err)
	}

	err = sysloggen.GenerateSyslog(sysloggen.Params{
		StartTime:     t,
		SecondLogTime: t2,

		LogBasename: "/tmp/nerdlog_agent_test_output/randomlog_huge",

		NumLogs:    40000000,
		MinDelayMS: 0,
		MaxDelayMS: 10,

		RandomSeed: 123,

		SkipIfPrevLogSizeIs: 143833610,
		SkipIfLastLogSizeIs: 2518973348,
	})
	if err != nil {
		return errors.Trace(err)
	}

	return nil
}
