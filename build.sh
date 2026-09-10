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
#
# This existed only in somebody's shell history before, which is why a fixed
# dependency could sit unreleased in the demo indefinitely.
set -eu

cd "$(dirname "$0")"

build_tinygo() {
	mkdir -p docs
	tinygo build -o docs/main.wasm -target wasm -no-debug ./cmd/websh
	cp "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" docs/wasm_exec.js
}

build_go() {
	mkdir -p docs/go
	GOOS=js GOARCH=wasm go build -o docs/go/main.wasm ./cmd/websh
	cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" docs/go/wasm_exec.js
}

case "${1:-both}" in
both)   build_tinygo; build_go ;;
tinygo) build_tinygo ;;
go)     build_go ;;
*)      echo "usage: $0 [both|tinygo|go]" >&2; exit 2 ;;
esac

ls -lh docs/main.wasm docs/go/main.wasm 2>/dev/null || true
