.PHONY: test vet build run clean e2e mock-tui

test:
	go test ./... -count=1

vet:
	go vet ./...

build:
	go build -o syla ./cmd/syla

run: build
	./syla --help

e2e:
	go test ./e2e -v -count=1

check: vet test

clean:
	rm -f syla
	rm -rf .syla
	go clean -testcache

install:
	go install ./cmd/syla

fmt:
	gofmt -w .

lint:
	go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then staticcheck ./...; fi

mock-tui:
	go run ./cmd/mock-tui
