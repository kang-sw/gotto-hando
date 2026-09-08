# gotto-hando build helpers. `make dev-sign` builds then applies the stable
# self-signed dev identity (help-macos.txt STABLE SIGNING IDENTITY) so TCC
# grants survive rebuilds. CGO stays disabled: the darwin/windows backends
# use purego, keeping cross-compilation easy (ffi.go).
BIN ?= gotto-hando

.PHONY: build sign dev-sign test vet

build:
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/gotto-hando

# Sign the built binary with the stable dev identity (macOS only).
sign:
	./scripts/codesign-dev.sh ./$(BIN)

# Build then sign in one step.
dev-sign: build sign

vet:
	go vet ./...

test:
	go test ./... -race
