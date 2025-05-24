VERSION != git describe --dirty --tags --always
COMMIT != git rev-parse HEAD
DATE != date -u +"%Y-%m-%dT%H:%M:%SZ"

all: clean nerdlog

.PHONY: nerdlog
nerdlog:
	@go build \
		-o bin/nerdlog \
		-ldflags "\
			-X 'github.com/dimonomid/nerdlog/version.version=$(patsubst v%,%,$(VERSION))' \
			-X 'github.com/dimonomid/nerdlog/version.commit=$(COMMIT)' \
			-X 'github.com/dimonomid/nerdlog/version.date=$(DATE)' \
			-X 'github.com/dimonomid/nerdlog/version.builtBy=make' \
		" \
		./cmd/nerdlog

.PHONY: clean
clean:
	@rm -rf bin

PREFIX ?= /usr/local
DESTDIR ?=
BINDIR := $(DESTDIR)$(PREFIX)/bin
INSTALL := install
INSTALL_FLAGS := -m 755

.PHONY: install
install:
	$(INSTALL) $(INSTALL_FLAGS) -D bin/nerdlog $(BINDIR)/nerdlog

# Run all tests. To skip the repetitions of the agent tests with shorter index
# file (expecting it to "index up"), because it's the slowest part of the
# tests, set env var NERDLOG_AGENT_TEST_SKIP_INDEX_UP
test:
	@# This step is needed to make sure that we don't get extra output like
	@# "go: downloading github.com/spf13/pflag v1.0.6" when running journalctl_mock,
	@# since it ends up in the debug output from the script which we then compare
	@# with the expected output.
	cd cmd/journalctl_mock && go build -o /dev/null
	@# The tests run rather slow so we use "-v -p 1" so that we get the unbuffered
	@# output.
	go test ./... -count 1 -v -p 1 $(ARGS)

# Run the tests (without the index-up repetitions), and update all the expected
# outputs in the repo.
#
# After running it, it's your job to examine the diff carefully, and if all the
# changes look legit, commit them.
update-test-expectations:
	rm -rf /tmp/nerdlog_agent_test_output /tmp/nerdlog_core_test_output
	-NERDLOG_AGENT_TEST_SKIP_INDEX_UP=1 make test
	bash util/copy_agent_test_results.sh
	bash util/copy_core_test_results.sh
	bash util/copy_e2e_test_results.sh

bench:
	# The -run=^$ is needed to avoid running the regular tests as well.
	go test ./core -bench=BenchmarkNerdlogAgent -benchtime=3s -run=^$
