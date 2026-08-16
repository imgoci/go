package main

import (
	"context"
	"os"
)

// main wires the process to run: streams and arguments become an env,
// terminating signals cancel the context, and the exit code run returns becomes
// the process status. This is the only function that touches the process, so
// the rest of the CLI is reachable from tests with buffers.
func main() {
	ctx, cancel := context.WithCancel(context.Background())

	stderr := guardStderr(os.Stderr)
	sig := watchSignals(cancel, stderr)
	code := run(ctx, env{args: os.Args[1:], stdout: os.Stdout, stderr: stderr}, sig)

	cancel()
	os.Exit(code)
}
