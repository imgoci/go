// Package e2e holds the end-to-end suite for the imgoci library: tests that
// drive the public github.com/imgoci/go API against real registries in
// containers, rather than against fakes.
//
// Every test file in this package is behind the e2e build tag, so the default
// `go test ./...` run skips it. Running the suite requires a Docker daemon,
// because the fixtures start zot and CNCF Distribution through
// testcontainers:
//
//	go test -race -tags e2e ./internal/e2e
//
// The suite lives outside the root package on purpose. It imports
// github.com/imgoci/go as an ordinary consumer would, which keeps it honest
// about what the library actually exports: a test here cannot reach an
// unexported seam to set up a scenario a real caller could not reach. Tests
// that do need such a seam belong beside the code they inspect, in the root
// package.
//
// Two environment variables control the bigoci CLI interop tests:
// IMGOCI_BIGOCI_CLI_DIR points at a local bigoci CLI module directory, and
// IMGOCI_BIGOCI_FORCE_CLONE=1 forces a shallow clone at the go.mod pin.
package e2e
