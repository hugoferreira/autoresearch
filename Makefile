#
# autoresearch — developer Makefile
#
# The most common commands:
#
#   make install   — go install ./cmd/autoresearch (drops `autoresearch`
#                    into $GOPATH/bin, which is how subagents find it on
#                    their $PATH). Run this once before opening any target
#                    project in Claude Code.
#
#   make build     — go build to ./autoresearch (gitignored). Useful for
#                    local CLI testing without clobbering the installed
#                    binary.
#
#   make test      — go vet + go test across the whole tree.
#
#   make tidy      — go mod tidy.
#
#   make clean     — rm the local build artifact.
#

.PHONY: install build test vet tidy clean

BIN ?= autoresearch

install:
	go install ./cmd/autoresearch
	@echo ""
	@echo "installed: $$(go env GOPATH)/bin/$(BIN)"
	@echo "verify:    $$(go env GOPATH)/bin/$(BIN) --help"
	@echo ""
	@echo "If 'autoresearch' is not on your PATH, add $$(go env GOPATH)/bin"
	@echo "to it — this is where subagents will look for it."

build: $(BIN)

$(BIN):
	go build -o $(BIN) ./cmd/autoresearch

test: vet
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BIN)
