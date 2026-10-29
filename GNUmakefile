SHELL        := bash
.SHELLFLAGS  := -euo pipefail -c

BUILD_DIR    ?= bin
BIN          := $(BUILD_DIR)/jdiff
TEST_TIMEOUT ?= 15s
# Installed, not run with go run: that may switch to a newer Go, whose
# standard library it would check instead of the one jdiff is built with.
# go install golang.org/x/vuln/cmd/govulncheck@latest
GOVULNCHECK  ?= $(shell go env GOPATH)/bin/govulncheck

.PHONY: all build test vet vuln fmt fmt-check clean

all: build

build: | $(BUILD_DIR)/
	go build -o $(BIN) . 2>&1 | tee $(BUILD_DIR)/build.log

# Sweeps, tests named ...Sweep, draw thousands of frames in one goroutine:
# the race detector finds nothing there but makes them ten times slower.
test: | $(BUILD_DIR)/
	timeout $(TEST_TIMEOUT) go test -race -count=1 -skip 'Sweep$$' ./... 2>&1 | tee $(BUILD_DIR)/test.log
	timeout $(TEST_TIMEOUT) go test -count=1 -run 'Sweep$$' ./... 2>&1 | tee -a $(BUILD_DIR)/test.log

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
