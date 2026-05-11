// Package version is the single source of truth for the shingan
// release the running binary was built for. The constant lives in its
// own package (rather than `cli` or `main`) so the plugin SDK can
// import it for compatibility checks without creating a cycle through
// the application/cli layers.
//
// Set at build time via ldflags:
//
//	go build -ldflags '-X github.com/hatyibei/shingan/version.Version=0.9.0' ./cmd/shingan
//
// The goreleaser configuration injects this automatically; manual `go
// install` and `go build` invocations get "dev".
//
// Stability commitment: this string is semver-formatted (without a
// leading `v`). It's part of the public Plugin SDK contract — plugin
// authors compare against it for compatibility — so the format won't
// change between v0.x minor versions.
package version

// Version is the shingan release tag this binary was built for, in
// semver form (e.g. "0.9.0"). Falls back to "dev" for development
// builds without ldflags injection.
var Version = "dev"

// IsDev reports whether the running binary is a development build
// (no version-string ldflag was set at build time). Compatibility
// checks treat dev builds as compatible with every plugin so local
// development isn't blocked by version-string mismatches.
func IsDev() bool { return Version == "dev" || Version == "" }
