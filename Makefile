BUF_VERSION := 1.46.0
PROTOC_GEN_GO_VERSION := 1.36.7
PROTOC_GEN_GO_GRPC_VERSION := 1.5.1

.PHONY: fmt fmt-check test test-race vet build tools generation-check check ci

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check:
	test -z "$$(gofmt -l .)"

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

build:
	go build -trimpath -o bin/jgo ./cmd/jgo

tools:
	go install github.com/bufbuild/buf/cmd/buf@v$(BUF_VERSION)
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v$(PROTOC_GEN_GO_VERSION)
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v$(PROTOC_GEN_GO_GRPC_VERSION)

generation-check:
	./scripts/verify-generation.sh

check: fmt-check test test-race vet

ci: check build generation-check
