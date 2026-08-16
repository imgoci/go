package main

import (
	"fmt"
	"io"
	"strconv"

	imgoci "github.com/imgoci/go"
)

// writeDeliverables writes the deterministic list listing: one line per
// stored transport alternative, in the order [imgoci.Index.List] already
// sorts. An empty match writes nothing.
func writeDeliverables(out io.Writer, deliverables []imgoci.Deliverable) error {
	for _, deliverable := range deliverables {
		for _, role := range deliverable.Roles {
			for _, alt := range role.Alternatives {
				if _, err := fmt.Fprintf(
					out, "%s\t%s\t%s\t%s\t%s\t%s\n",
					deliverable.Architecture,
					deliverable.Target,
					deliverable.Representation,
					role.Role,
					alt.Compression,
					alt.ArtifactType,
				); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// writeResolved writes the deterministic resolve listing: one line per
// selected role, in [imgoci.Resolved.Entries] order.
func writeResolved(out io.Writer, sel *imgoci.Resolved) error {
	for _, entry := range sel.Entries() {
		if _, err := fmt.Fprintf(
			out, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			entry.Selector.Architecture,
			entry.Selector.Target,
			entry.Selector.Representation,
			entry.Selector.Role,
			entry.Selector.Compression,
			entry.Filename,
			entry.ArtifactType,
			entry.ContentDigest.String(),
			strconv.FormatInt(entry.ContentSize, 10),
		); err != nil {
			return err
		}
	}

	return nil
}
