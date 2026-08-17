package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	imgoci "github.com/imgoci/go"
)

// runFetch parses fetch's command line, selects one deliverable, and writes
// verified files into dest. Nothing but the help block goes to standard output.
func runFetch(ctx context.Context, e env, args []string) error {
	var f fetchFlags
	fs := newFlagSet(cmdFetch)
	f.register(fs)

	operands, err := command{
		flags:    fs,
		name:     cmdFetch,
		syntax:   "<ref> <dest>",
		usage:    fetchUsage(),
		operands: twoOperands,
	}.parse(e, args)
	if err != nil {
		return err
	}
	ref, dest := operands[0], operands[1]

	set := setFlagNames(fs)
	if validateErr := f.common.validate(set, cmdFetch, fetchUsage()); validateErr != nil {
		return validateErr
	}
	if requireErr := f.query.requireResolveSelectors(cmdFetch, fetchUsage()); requireErr != nil {
		return requireErr
	}

	query, err := f.query.resolveQuery()
	if err != nil {
		return usageErrorf(fetchUsage(), "fetch: %s", err)
	}

	client, err := newClient(f.common)
	if err != nil {
		return err
	}
	fmt.Fprintf(
		e.stderr, "imgoci: fetch %s -> %s (%s)\n",
		terminalSafeLine(ref), terminalSafeLine(dest), f.common.settings(set),
	)

	watch := startProgress(e, f.common.progress)
	opts := f.common.workerOptions(set)
	if watch != nil {
		opts = append(opts, imgoci.WithProgress(watch.record))
	}

	started := time.Now()
	err = withDeadline(ctx, f.common.timeout, func(ctx context.Context) error {
		rel, fetchErr := client.Fetch(ctx, imgoci.Reference(ref))
		if fetchErr != nil {
			return fetchErr
		}
		sel, resolveErr := client.Resolve(rel, query)
		if resolveErr != nil {
			return resolveErr
		}

		return client.FetchFiles(ctx, rel, sel, imgoci.ToDir(dest), opts...)
	})

	watch.stop()
	if err != nil {
		return err
	}

	fmt.Fprintf(e.stderr, "imgoci: fetched in %s\n", time.Since(started).Round(resultPrecision))

	return nil
}

// fetchUsage is fetch's usage text.
func fetchUsage() string {
	var f fetchFlags
	fs := newFlagSet(cmdFetch)
	f.register(fs)

	return usageBlock(`usage: imgoci fetch [flags] <ref> <dest>

Fetch the release index <ref> names, select one deliverable, and write the
verified files into directory <dest>, named by io.imgoci.filename. Standard
output stays empty.

-architecture, -target, -representation, and at least one -compression are
required and are checked before any network I/O. Unset -usage selects the
empty usage set. Diagnostics and optional progress go to stderr.

flags:
`, fs)
}

// fetchFlags holds what a fetch command line asked for.
type fetchFlags struct {
	// common are the flags every registry command declares.
	common commonFlags
	// query are the required resolve selectors.
	query queryFlags
}

// register declares fetch's flags on fs.
func (f *fetchFlags) register(fs *flag.FlagSet) {
	f.common.register(fs)
	f.common.registerWorkers(fs)
	f.common.registerProgress(fs)
	f.query.registerResolve(fs)
}
