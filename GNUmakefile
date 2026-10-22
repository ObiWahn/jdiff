SHELL        := bash
.SHELLFLAGS  := -euo pipefail -c

BUILD_DIR    ?= bin
BIN          := $(BUILD_DIR)/jdiff
TEST_TIMEOUT ?= 15s

.PHONY: all build test vet fmt fmt-check clean

all: build

build: | $(BUILD_DIR)/
	go build -o $(BIN) . 2>&1 | tee $(BUILD_DIR)/build.log

test: | $(BUILD_DIR)/
	timeout $(TEST_TIMEOUT) go test -race -count=1 ./... 2>&1 | tee $(BUILD_DIR)/test.log

vet: | $(BUILD_DIR)/
	go vet ./... 2>&1 | tee $(BUILD_DIR)/vet.log

fmt:
	gofmt -w .

fmt-check: | $(BUILD_DIR)/
	test -z "$$(gofmt -l . | tee $(BUILD_DIR)/fmt.log)"

clean:
	rm -rf $(BUILD_DIR)

# Directory targets end in '/' so they never clash with phony targets of the same name.
%/:
	mkdir -p $@
