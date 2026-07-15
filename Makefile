export GOTOOLCHAIN := local
export GOWORK := off

.PHONY: fmt fmt-check mod-verify mod-tidy-check test test-race vet build tools generation-check check ci

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check:
	test -z "$$(gofmt -l .)"

mod-verify:
	go mod verify

mod-tidy-check:
	go mod tidy -diff

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

build:
	go build -trimpath -o bin/jgo ./cmd/jgo

tools:
	go run ./cmd/jgo tools install

generation-check:
	./scripts/verify-generation.sh

check: fmt-check mod-verify mod-tidy-check test test-race vet

ci: check build generation-check
