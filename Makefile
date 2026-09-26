BINARY := knowledge-mcp
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS := -ldflags "-X main.version=$(VERSION)"
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build build-all test clean install lint fmt tidy eval-mock eval

all: fmt tidy lint test build

build:
	go build $(GOFLAGS) -o $(BINARY) .

build/%:
	@$(MAKE) build VERSION=$*

build-all:
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		output=$(BINARY)-$$os-$$arch; \
		if [ "$$os" = "windows" ]; then output=$$output.exe; fi; \
		echo "Building $$output..."; \
		GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -o dist/$$output . || exit 1; \
	done

test:
	go test ./...

clean:
	rm -f $(BINARY)
	rm -rf dist/

install:
	go install $(GOFLAGS) .

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		go vet ./...; \
	fi

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

eval-mock:
	go test ./eval/ -v -run 'TestRunMock|TestScenarioCorpusValid|TestLoadMockEvidence'

eval:
	perl evals/run.pl $(if $(SCENARIO),--scenario $(SCENARIO)) $(if $(TAG),--tag $(TAG)) $(if $(MODEL),--model $(MODEL)) --xml ./tmp/docs/eval-results.xml
