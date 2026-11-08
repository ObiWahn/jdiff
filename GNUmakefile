SHELL        := bash
.SHELLFLAGS  := -euo pipefail -c

BUILD_DIR    ?= bin
BIN          := $(BUILD_DIR)/jdiff
TEST_TIMEOUT ?= 15s
RACE_TIMEOUT ?= 40s
# Compiling the tests, with -race too, before the timed runs: a cold build
# cache takes longer than the runs themselves.
COMPILE_TIMEOUT ?= 300s
# Installed, not run with go run: that may switch to a newer Go, whose
# standard library it would check instead of the one jdiff is built with.
# go install golang.org/x/vuln/cmd/govulncheck@latest
GOVULNCHECK  ?= $(shell go env GOPATH)/bin/govulncheck

.PHONY: all build test test-race cover licenses tag vet vuln fmt fmt-check clean

all: build

build: | $(BUILD_DIR)/
	go build -o $(BIN) . 2>&1 | tee $(BUILD_DIR)/build.log

# Sweeps, tests named ...Sweep, draw thousands of frames in one goroutine:
# the race detector finds nothing there but makes them ten times slower.
test: | $(BUILD_DIR)/
	timeout $(COMPILE_TIMEOUT) go test -race -count=1 -run '^$$' ./... 2>&1 | tee $(BUILD_DIR)/test.log
	timeout $(COMPILE_TIMEOUT) go test -count=1 -run '^$$' ./... 2>&1 | tee -a $(BUILD_DIR)/test.log
	timeout $(TEST_TIMEOUT) go test -race -count=1 -skip 'Sweep$$' ./... 2>&1 | tee -a $(BUILD_DIR)/test.log
	timeout $(TEST_TIMEOUT) go test -count=1 -run 'Sweep$$' ./... 2>&1 | tee -a $(BUILD_DIR)/test.log

# All tests with the race detector, the sweeps too: before a release.
test-race: | $(BUILD_DIR)/
	timeout $(COMPILE_TIMEOUT) go test -race -count=1 -run '^$$' ./... 2>&1 | tee $(BUILD_DIR)/test-race.log
	timeout $(RACE_TIMEOUT) go test -race -count=1 ./... 2>&1 | tee -a $(BUILD_DIR)/test-race.log

# Coverage over all packages: settings has no tests of its own, ui tests it.
# The profile has each block once per package tested: count it once, as run
# if any of them ran it.
cover: | $(BUILD_DIR)/
	timeout $(TEST_TIMEOUT) go test -count=1 -coverpkg=./... -coverprofile=$(BUILD_DIR)/coverage.out ./... 2>&1 | tee $(BUILD_DIR)/cover.log
	awk 'NR > 1 { size[$$1] = $$2; if ($$3 > 0) ran[$$1] = 1 } \
		END { for (b in size) { p = b; sub(/\/[^\/]*$$/, "", p); all[p] += size[b]; if (b in ran) hit[p] += size[b] } \
			for (p in all) printf "%-24s %5.1f%%\n", p, 100 * hit[p] / all[p] }' $(BUILD_DIR)/coverage.out | sort > $(BUILD_DIR)/coverage.txt
	echo >> $(BUILD_DIR)/coverage.txt
	go tool cover -func=$(BUILD_DIR)/coverage.out >> $(BUILD_DIR)/coverage.txt
	tail -n 1 $(BUILD_DIR)/coverage.txt | tee -a $(BUILD_DIR)/cover.log
	go tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html

# Writes LICENSE-THIRD-PARTY, the licenses of the linked modules, after a
# change of dependencies; make test fails until then.
licenses: | $(BUILD_DIR)/
	timeout $(TEST_TIMEOUT) go test -count=1 -run '^TestThirdPartyLicenses$$' . -update 2>&1 | tee $(BUILD_DIR)/licenses.log

# Tags HEAD of master with the next version, the last tag vX.Y.Z with Z one
# higher, when the tree is clean, HEAD has no tag yet and test-race passes.
# Nothing is pushed: pushing the tag publishes the release.
tag:
	test "$$(git branch --show-current)" = master || { echo "tag: not on master" >&2; exit 1; }
	test -z "$$(git status --porcelain)" || { echo "tag: uncommitted changes" >&2; exit 1; }
	test -z "$$(git tag --points-at HEAD --list 'v*')" || \
		{ echo "tag: HEAD is tagged already: $$(git tag --points-at HEAD --list 'v*')" >&2; exit 1; }
	$(MAKE) test-race
	last=$$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | head -n 1) || \
		{ echo "tag: no tag vX.Y.Z yet" >&2; exit 1; }; \
	next=$${last%.*}.$$(( $${last##*.} + 1 )); \
	git tag "$$next"; \
	echo "tagged $$next; publish it with: git push <remote> $$next"

vet: | $(BUILD_DIR)/
	go vet ./... 2>&1 | tee $(BUILD_DIR)/vet.log

# Known vulnerabilities in the code jdiff calls, from https://vuln.go.dev.
vuln: | $(BUILD_DIR)/
	$(GOVULNCHECK) ./... 2>&1 | tee $(BUILD_DIR)/vuln.log

fmt:
	gofmt -w .

fmt-check: | $(BUILD_DIR)/
	test -z "$$(gofmt -l . | tee $(BUILD_DIR)/fmt.log)"

clean:
	rm -rf $(BUILD_DIR)

# Directory targets end in '/' so they never clash with phony targets of the same name.
%/:
	mkdir -p $@
