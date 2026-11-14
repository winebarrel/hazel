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
