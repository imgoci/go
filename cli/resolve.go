package main

import (
	"context"
	"flag"
	"fmt"

	imgoci "github.com/imgoci/go"
)

// runResolve parses resolve's command line, fetches the index, selects one
// deliverable, and writes the deterministic listing.
func runResolve(ctx context.Context, e env, args []string) error {
	var f resolveFlags
	fs := newFlagSet(cmdResolve)
	f.register(fs)

	operands, err := command{
		flags:    fs,
		name:     cmdResolve,
		syntax:   "<ref>",
		usage:    resolveUsage(),
		operands: 1,
	}.parse(e, args)
	if err != nil {
		return err
	}
	ref := operands[0]

	set := setFlagNames(fs)
	if validateErr := f.common.validate(set, cmdResolve, resolveUsage()); validateErr != nil {
		return validateErr
	}
	if requireErr := f.query.requireResolveSelectors(cmdResolve, resolveUsage()); requireErr != nil {
		return requireErr
	}

	query, err := f.query.resolveQuery()
	if err != nil {
		return usageErrorf(resolveUsage(), "resolve: %s", err)
	}

	client, err := newClient(f.common)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stderr, "imgoci: resolve %s\n", terminalSafeLine(ref))

	var sel *imgoci.Resolved
	err = withDeadline(ctx, f.common.timeout, func(ctx context.Context) error {
		rel, fetchErr := client.Fetch(ctx, imgoci.Reference(ref))
		if fetchErr != nil {
			return fetchErr
		}
		resolved, resolveErr := client.Resolve(rel, query)
		sel = resolved

		return resolveErr
	})
	if err != nil {
		return err
	}

	return writeResolved(e.stdout, sel)
}

// resolveUsage is resolve's usage text.
func resolveUsage() string {
	var f resolveFlags
	fs := newFlagSet(cmdResolve)
	f.register(fs)

	return usageBlock(`usage: imgoci resolve [flags] <ref>

Fetch the release index <ref> names, select one deliverable, and write each
selected role as a tab-separated line:

  <architecture>	<target>	<representation>	<usage>	<role>	<compression>	<filename>	<artifactType>	<contentDigest>	<contentSize>

-architecture, -target, -representation, and at least one -compression are
required and are checked before any network I/O. Unset -usage selects the
empty usage set. Diagnostics go to stderr.

flags:
`, fs)
}

// resolveFlags holds what a resolve command line asked for.
type resolveFlags struct {
	// common are the flags every registry command declares.
	common commonFlags
	// query are the required resolve selectors.
	query queryFlags
}

// register declares resolve's flags on fs.
func (r *resolveFlags) register(fs *flag.FlagSet) {
	r.common.register(fs)
	r.query.registerResolve(fs)
}
