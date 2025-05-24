# Tests

## Quick start

To run all tests:

```
make test
```

To skip indexing-up agent tests (which are the slowest):

```
NERDLOG_AGENT_TEST_SKIP_INDEX_UP=1 make test
```

## Details

There are 4 kinds of tests:

- Regular Go unit tests
- Agent script tests
- Core tests
- End-to-end tests

Let's talk about them in more detail:

### Regular Go unit tests

They specify test cases right in Go code and cover some small part of functionality in detail. Not much more to say about them.

### Agent script tests

These tests cover the behavior of the agent script (`../core/nerdlog_agent.sh`), which is arguably the most tricky (and as of May 2025, also the most dirty) part. It's basically the backend which has all the shell hacks to do the actual log filtering and processing.

They run as a Go test func `TestNerdlogAgent` (in `../core/nerdlog_agent_test.go`), but the actual test cases are defined under `../core/core_testdata/test_cases_agent` in various `test_case.yaml` files.

Every test case specifies the arguments to run the agent script with, and the exact expected stdout and stderr. On how to update these expected outputs conveniently when the output changes in some way, see below.

As the tests run, the outputs are written to `/tmp/nerdlog_agent_test_output`.

It's important to note that these tests are not isolated to nerdlog: they use a bunch of tools from the environment such as `bash`, `gawk`, `tail`, `head` etc, so you need to have all of them installed for the tests to work. As a consequence, these tests make sure that nerdlog works *on your particular environment*. They are expected to work at least on Linux, FreeBSD and MacOS (CI runs tests on these platforms).

#### Test cases for plain log files

Every test case for plain log files (as opposed to `journalctl`) runs multiple times: first without the index, so that the index gets created from scratch; and then also multiple times with smaller and smaller index, and we expect the index to be automatically updated as needed, and the resulting logs (i.e. `stdout`) to be exactly the same.

This is the slowest part of the tests though, so to skip these index-up repetitions, set the env var `NERDLOG_AGENT_TEST_SKIP_INDEX_UP` to 1.

Even though `stdout` is the same when we're running the same test with an incomplete index, `stderr` is not: the output will be different based on the index file. So the `stderr` is only checked during the first run; and then during these index-up repetitions, we only check `stdout`.

#### Test cases for journalctl

We use a mocked journalctl for these test cases, see `../cmd/journalctl_mock`. There are no index-up repetitions for these test cases, because there is no nerdlog-maintained index.

These tests run even on platforms without `journalctl` (such as FreeBSD and MacOS), since the mock is cross-platform.

### Core tests

These cover not only the agent script, but also `LStreamClient`, `LStreamsManager`, and all the helpers. Basically, almost everything in the `../core` package, thus the name.

They run as a Go test func `TestCoreScenarios` (in `../core/core_test.go`), but the actual test cases are defined under `../core/core_testdata/test_cases_core` in various `test_scenario.yaml` files.

Every test scenario specifies the logstreams to connect to, and then a few steps with queries to these logstreams, and every step has the expected human-readable output, which includes the timeline histogram buckets, the logs, debug output, etc. On how to update these expected outputs conveniently when the output changes in some way, see below.

As the tests run, the outputs are written to `/tmp/nerdlog_core_test_output`.

Arguably, these core tests could potentially replace the agent tests, because they cover the agent too; however agent tests drill down in more agent-specific details, and rewriting the equivalent in core tests might be tricky, so there are no plans to do that.

And the same note about the environment applies here: just like agent tests, core tests rely on a few tools from your environment.

By default, the hostname `localhost` is being used in these core tests, which means that we use `ShellTransportLocal`. If the env var `NERDLOG_CORE_TEST_HOSTNAME` is set though, then it will be used instead of `localhost`, and on CI we run a separate job setting it to `127.0.0.1`, like this:

```
NERDLOG_CORE_TEST_HOSTNAME='127.0.0.1' make test ARGS='-run TestCoreScenarios'
```

Which causes tests to establish an actual ssh connection, and thus we cover the `ShellTransportSSH` as well. For this to work for you locally, you obviously need to have an ssh server running locally, and you need to be able to `ssh 127.0.0.1` without a password via ssh agent.

### End-to-end tests

Those run the final `nerdlog` binary, capture the actual TUI screen snapshots using `tmux`, and compare them with the expected output.

They run as a Go test func `TestE2EScenarios` (in `../cmd/nerdlog/e2e_test.go`), but the actual test cases are defined under `../cmd/nerdlog/e2e_testdata` in various `test_scenario.yaml` files.

As the tests run, the outputs are written to `/tmp/nerdlog_e2e_test_output`.

These tests don't go into much details, but they provide a good end-to-end smoke test to make sure we don't break things in some silly way.

### Updating expected outputs

Since all tests here except unit tests specify the exact expected outputs, it means that when we change the format of these outputs in some way, even change some debug print, we need to update the affected test cases as well. There is a convenient helper for that:

```
make update-test-expectations
```

It will run all the tests, and copy the actual outputs from `/tmp/nerdlog_agent_test_output`, `/tmp/nerdlog_core_test_output` and `/tmp/nerdlog_e2e_test_output` to the repository.

After that, it's your job to examine the output `git diff` carefully, and if all the changes in the expected outputs look legit, commit them.

One extra note here: as you remember, in agent tests we run every case multiple times, expecting `stdout` to be the same; but we only check `stderr` the first time (without index), because it'll be different on these repetitions. But here for updating the test expectations, we need to generate `stderr` specifically after the first iteration, thus `make update-test-expectations` sets `NERDLOG_AGENT_TEST_SKIP_INDEX_UP=1`. Just something to keep in mind.
