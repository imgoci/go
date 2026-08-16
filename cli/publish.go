package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	imgoci "github.com/imgoci/go"
)

// runPublish parses publish's command line and runs the publish it describes.
//
// The spec is decoded and required members are checked before a client is
// built. The reference is passed through exactly as typed; the CLI does not
// parse reference grammar.
func runPublish(ctx context.Context, e env, args []string) error {
	var f publishFlags
	fs := newFlagSet(cmdPublish)
	f.register(fs)

	operands, err := command{
		flags:    fs,
		name:     cmdPublish,
		syntax:   "<spec> <ref>",
		usage:    publishUsage(),
		operands: twoOperands,
	}.parse(e, args)
	if err != nil {
		return err
	}
	specPath, ref := operands[0], operands[1]

	set := setFlagNames(fs)
	if validateErr := f.common.validate(set, cmdPublish, publishUsage()); validateErr != nil {
		return validateErr
	}

	spec, err := loadReleaseSpec(specPath)
	if err != nil {
		return usageErrorf(publishUsage(), "publish: %s", err)
	}

	client, err := newClient(f.common)
	if err != nil {
		return err
	}
	fmt.Fprintf(
		e.stderr, "imgoci: publish %s -> %s (%s)\n",
		terminalSafeLine(specPath), terminalSafeLine(ref), f.common.settings(set),
	)

	watch := startProgress(e, f.common.progress)
	opts := f.common.publishWorkerOptions(set)
	if watch != nil {
		opts = append(opts, imgoci.WithProgress(watch.record))
	}

	started := time.Now()
	var digest string
	err = withDeadline(ctx, f.common.timeout, func(ctx context.Context) error {
		published, publishErr := client.Publish(ctx, imgoci.Reference(ref), spec, opts...)
		digest = published.String()

		return publishErr
	})

	watch.stop()
	if err != nil {
		return err
	}

	return writePublished(e.stdout, e.stderr, digest, time.Since(started))
}

// writePublished writes the index digest to stdout and the success diagnostic
// to stderr. A stdout write failure is returned before the diagnostic so a
// closed pipe cannot look like a successful publish.
func writePublished(stdout, stderr io.Writer, digest string, elapsed time.Duration) error {
	if _, err := fmt.Fprintln(stdout, digest); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "imgoci: published %s in %s\n", digest, elapsed.Round(resultPrecision))

	return nil
}

// publishUsage is publish's usage text.
func publishUsage() string {
	var f publishFlags
	fs := newFlagSet(cmdPublish)
	f.register(fs)

	return usageBlock(`usage: imgoci publish [flags] <spec> <ref>

Read the JSON publish spec at <spec>, publish it at the tag-only reference
<ref>, and write the canonical index digest to stdout on a line of its own.
Everything else goes to stderr.

<spec> maps losslessly onto imgoci.ReleaseSpec. Unknown JSON members are
rejected. Relative file paths are resolved against the directory that contains
<spec>. See the package comment for the document shape.

flags:
`, fs)
}

// publishFlags holds what a publish command line asked for.
type publishFlags struct {
	// common are the flags every registry command declares.
	common commonFlags
}

// register declares publish's flags on fs.
func (p *publishFlags) register(fs *flag.FlagSet) {
	p.common.register(fs)
	p.common.registerWorkers(fs)
	p.common.registerProgress(fs)
}
