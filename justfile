# `just install` puts rook on your PATH. Everything else here is a shortcut.
#
# ponytail: `go install` already does the work — GOPATH/bin is on PATH by
# default on a Go install, so there is no copy step and no sudo. The one thing
# worth wrapping is that installing a new binary changes nothing until the old
# daemon goes away.

_default:
    @just --list --unsorted

# build ./rook in the repo, for trying a change without installing it
build:
    go build -o rook ./cmd/rook

# put rook on your PATH and restart the daemon so it takes effect
install:
    go install ./cmd/rook
    @rook kill >/dev/null 2>&1 || true
    @echo "installed: $(command -v rook)"
    @rook --version

# take it off your PATH again
uninstall:
    @rm -f "$(go env GOPATH)/bin/rook" && echo "removed"
    @rook kill >/dev/null 2>&1 || true

test:
    go test ./...

# ponytail: `gofmt -l` exits 0 even when it lists files, so it has to be tested
check:
    @test -z "$(gofmt -l .)" || { echo "needs gofmt:"; gofmt -l .; exit 1; }
    go vet ./...
    go test ./...

# stop the daemon; the next `rook` starts a fresh one
kill:
    -rook kill

# wire up the agent side: hooks plus the skill, in the config Claude loads
setup: install
    rook integration install claude
    rook skill --install
    rook integration status
