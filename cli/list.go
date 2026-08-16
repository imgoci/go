package main

import (
	"context"
	"flag"
	"fmt"

	imgoci "github.com/imgoci/go"
)

// runList parses list's command line, fetches the index, and writes the
// deterministic listing.
func runList(ctx context.Context, e env, args []string) error {
	var f listFlags
	fs := newFlagSet(cmdList)
	f.register(fs)

	operands, err := command{
		flags:    fs,
		name:     cmdList,
		syntax:   "<ref>",
		usage:    listUsage(),
		operands: 1,
	}.parse(e, args)
	if err != nil {
		return err
	}
	ref := operands[0]

	set := setFlagNames(fs)
	if validateErr := f.common.validate(set, cmdList, listUsage()); validateErr != nil {
		return validateErr
	}

	client, err := newClient(f.common)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stderr, "imgoci: list %s\n", terminalSafeLine(ref))

	var rel *imgoci.Release
	err = withDeadline(ctx, f.common.timeout, func(ctx context.Context) error {
		fetched, fetchErr := client.Fetch(ctx, imgoci.Reference(ref))
		rel = fetched

		return fetchErr
	})
	if err != nil {
		return err
	}

	deliverables, err := rel.Index().List(f.query.listQuery())
	if err != nil {
		return err
	}

	return writeDeliverables(e.stdout, deliverables)
}

// listUsage is list's usage text.
func listUsage() string {
	var f listFlags
	fs := newFlagSet(cmdList)
	f.register(fs)

	return usageBlock(`usage: imgoci list [flags] <ref>

Fetch the release index <ref> names and write every matching deliverable as
tab-separated lines:

  <architecture>	<target>	<representation>	<role>	<compression>	<artifactType>

Empty filters match every value. An empty match prints nothing. Diagnostics
go to stderr.

flags:
`, fs)
}

// listFlags holds what a list command line asked for.
type listFlags struct {
	// common are the flags every registry command declares.
	common commonFlags
	// query are the optional list filters.
	query queryFlags
}

// register declares list's flags on fs.
func (l *listFlags) register(fs *flag.FlagSet) {
	l.common.register(fs)
	l.query.registerList(fs)
}
