#!/bin/sh
# Build the web view into docs/, which is what GitHub Pages serves.
#
# Both toolchains are carried: TinyGo at docs/ because it is a fraction of the
# size and this is fetched over the network before anything appears, and the
# standard Go build at docs/go/ because TinyGo occasionally miscompiles
# something and having the other one a click away is how you find out that is
# what happened.
#
#   ./build.sh          both
#   ./build.sh tinygo   TinyGo only
#   ./build.sh go       standard Go only
#   ./build.sh demo     the program run from the filesystem, docs/bin/ttydemo.wasm
#   ./build.sh term     the terminal package term embeds, term/term.wasm.gz
#
# This existed only in somebody's shell history before, which is why a fixed
# dependency could sit unreleased in the demo indefinitely.
set -eu

cd "$(dirname "$0")"

# The version programs are told (Discovery): stamped, because released
# TinyGo records no module information (tinygo-org/tinygo#5592 adds it).
version() { git describe --always --dirty 2>/dev/null || echo dev; }

build_tinygo() {
	mkdir -p docs
	tinygo build -o docs/main.wasm -target wasm -no-debug -ldflags "-X github.com/0magnet/websh/web.Version=$(version)" ./cmd/websh
	cp "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" docs/wasm_exec.js
}

build_go() {
	mkdir -p docs/go
	GOOS=js GOARCH=wasm go build -o docs/go/main.wasm ./cmd/websh
	cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" docs/go/wasm_exec.js
}

# bottle's page scripts — the filesystem, the loopback network and processes —
# at the version go.mod names, so the page and the Go that talks to them agree.
bottle_js() {
	GOFLAGS=-mod=mod go mod download github.com/0magnet/bottle
	dir=$(GOFLAGS=-mod=mod go list -m -f '{{.Dir}}' github.com/0magnet/bottle)
	[ -n "$dir" ] || { echo 'bottle: module not found' >&2; exit 1; }
	for f in jsfs.js vnet.js proc.js update.js; do cp "$dir/$f" "docs/$f"; chmod 644 "docs/$f"; done
}

# A program that is not part of the page: fetched into the shell and run from
# the PATH, as a process of its own (curl -o /bin/ttydemo .../bin/ttydemo.wasm).
build_demo() {
	mkdir -p docs/bin
	tinygo build -o docs/bin/ttydemo.wasm -target wasm -no-debug ./cmd/ttydemo
	tinygo build -o docs/bin/wasmwidget.wasm -target wasm -no-debug ./cmd/wasmwidget
	tinygo build -o docs/bin/progcheck.wasm -target wasm -no-debug ./cmd/progcheck
	tinygo build -o docs/bin/progdemo.wasm -target wasm -no-debug ./cmd/progdemo
}

# The terminal package term embeds: TinyGo for its size, gzipped once here so
# the server sends it as it is. wasm_exec.js must come from the same TinyGo.
build_term() {
	tinygo build -o term/term.wasm -target wasm -no-debug -ldflags "-X github.com/0magnet/websh/web.Version=$(version)" ./cmd/websh-term
	gzip -9 -n -f term/term.wasm
	cp "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" term/wasm_exec.js
	chmod 644 term/wasm_exec.js term/term.wasm.gz
	ls -lh term/term.wasm.gz
}

# The stamp bottle's update.js watches (docs/index.html): a hash of what was
# built, so the open page hears of a new build when a binary changes.
stamp() {
	cat docs/*.wasm docs/go/*.wasm docs/bin/*.wasm docs/*.js docs/go/*.js 2>/dev/null | sha256sum | cut -c1-16 > docs/version.txt
}
case "${1:-both}" in
both)   bottle_js; build_tinygo; build_go; build_demo; stamp ;;
tinygo) bottle_js; build_tinygo; stamp ;;
go)     bottle_js; build_go; stamp ;;
demo)   build_demo; stamp ;;
term)   build_term ;;
*)      echo "usage: $0 [both|tinygo|go|demo|term]" >&2; exit 2 ;;
esac

ls -lh docs/main.wasm docs/go/main.wasm 2>/dev/null || true
