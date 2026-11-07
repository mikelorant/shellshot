// Package ghostty implements vt.Terminal on top of libghostty-vt through the
// cgo bindings in go.mitchellh.com/libghostty.
//
// # Linkage
//
// This is the only package in the program that imports cgo. The bindings
// locate libghostty-vt with pkg-config, so building or testing anything that
// imports this package needs the library from scripts/fetch-libghostty.sh,
// which unpacks it into third_party/libghostty. Point pkg-config at it:
//
//	PKG_CONFIG_PATH=$PWD/third_party/libghostty/lib/pkgconfig go test ./...
//
// The Makefile sets that variable. By default the static library is linked,
// so the resulting binary has no runtime dependency on libghostty.
//
// # Concurrency
//
// A Terminal is not safe for concurrent use; callers serialise access, as the
// vt.Terminal contract requires.
package ghostty
