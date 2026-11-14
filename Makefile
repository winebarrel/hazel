.PHONY: all
all: vet test build

.PHONY: build
build:
	go build ./cmd/hazel

.PHONY: install
install:
	go install ./cmd/hazel

.PHONY: vet
vet:
	go vet ./...

.PHONY: test
test:
	go test -v ./... $(TEST_OPTS)

.PHONY: lint
lint:
	golangci-lint run

.PHONY: fix
fix:
	golangci-lint run --fix

include realworld.mk

# Records demo.gif and demo.webm from demo.tape with vhs
# (https://github.com/charmbracelet/vhs), which needs ttyd and ffmpeg. The
# demo pages through bat. The recordings are not committed.
.PHONY: demo
demo: build
	vhs demo.tape

# Tests scripts/merge-changelog.sh, the CHANGELOG.md merge driver that
# .github/workflows/changelog-merge.yml runs.
.PHONY: test-changelog
test-changelog:
	bash test/changelog/run.sh
