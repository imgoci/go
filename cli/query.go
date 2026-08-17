package main

import (
	"flag"
	"fmt"

	imgoci "github.com/imgoci/go"
)

// queryFlags are the filters and preferences list, resolve, and fetch share.
type queryFlags struct {
	// architecture is an exact architecture filter or selector.
	architecture string
	// target is an exact target filter or selector.
	target string
	// representation is an exact representation filter or selector.
	representation string
	// usage is the list containment filter or the exact resolve set.
	usage stringList
	// roles are the required or selected roles, in the order they were set.
	roles stringList
	// compressions are the accepted compressions, most preferred first.
	compressions stringList
	// capabilities are extra consumer file-manifest types.
	capabilities stringList
}

// registerList declares the optional filters list accepts.
func (q *queryFlags) registerList(fs *flag.FlagSet) {
	fs.StringVar(&q.architecture, flagArchitecture, "", "exact architecture filter (unset: match every architecture)")
	fs.StringVar(&q.target, flagTarget, "", "exact target filter (unset: match every target)")
	fs.StringVar(&q.representation, flagRepresentation, "",
		"exact representation filter (unset: match every representation)")
	fs.Var(&q.usage, flagUsage,
		"usage value a matching set must contain; repeat to require several (unset: match every usage set)")
	fs.Var(&q.roles, flagRole, "require this role; repeat to require several (unset: no role filter)")
}

// registerResolve declares the required selectors resolve and fetch accept.
func (q *queryFlags) registerResolve(fs *flag.FlagSet) {
	fs.StringVar(&q.architecture, flagArchitecture, "", "required exact architecture selector")
	fs.StringVar(&q.target, flagTarget, "", "required exact target selector")
	fs.StringVar(&q.representation, flagRepresentation, "", "required exact representation selector")
	fs.Var(&q.usage, flagUsage,
		"one value of the complete exact usage set; repeat to name the set (unset: the empty usage set)")
	fs.Var(&q.roles, flagRole, "select this role; repeat to select several (unset: the default-role rule)")
	fs.Var(&q.compressions, flagCompression, "accepted compression, most preferred first; repeat to accept several")
	fs.Var(&q.capabilities, flagCapability,
		"consumer file-manifest type; repeat to accept several (unset: the client's capabilities)")
}

// listQuery maps the flags onto [imgoci.ListQuery]. Unset scalars stay empty
// and match every value. A nil Roles or Usage slice applies no filter.
func (q *queryFlags) listQuery() imgoci.ListQuery {
	return imgoci.ListQuery{
		Architecture:   q.architecture,
		Target:         q.target,
		Representation: q.representation,
		Usage:          q.usage.values,
		Roles:          q.roles.values,
	}
}

// resolveQuery maps the flags onto [imgoci.ResolveQuery]. A zero Capabilities
// value lets [imgoci.Client.Resolve] default to the client's own set.
func (q *queryFlags) resolveQuery() (imgoci.ResolveQuery, error) {
	query := imgoci.ResolveQuery{
		Architecture:   q.architecture,
		Target:         q.target,
		Representation: q.representation,
		Usage:          q.usage.values,
		Roles:          q.roles.values,
		Compressions:   q.compressions.values,
	}
	if len(q.capabilities.values) == 0 {
		return query, nil
	}

	caps, err := imgoci.NewCapabilities(q.capabilities.values...)
	if err != nil {
		return imgoci.ResolveQuery{}, err
	}
	query.Capabilities = caps

	return query, nil
}

// requireResolveSelectors rejects a resolve or fetch command line that is
// missing a required selector or compression preference. The check runs
// before a registry adapter is built.
func (q *queryFlags) requireResolveSelectors(name, usage string) error {
	if q.architecture == "" {
		return usageErrorf(usage, "%s requires -architecture", name)
	}
	if q.target == "" {
		return usageErrorf(usage, "%s requires -target", name)
	}
	if q.representation == "" {
		return usageErrorf(usage, "%s requires -representation", name)
	}
	if len(q.compressions.values) == 0 {
		return usageErrorf(usage, "%s requires at least one -compression", name)
	}

	return nil
}

// stringList is a repeatable string flag. The zero value is a nil slice, which
// is the library's "omitted" meaning for Roles.
type stringList struct {
	// values are the flag occurrences in the order they were set.
	values []string
}

// String renders the collected values for [flag.FlagSet.PrintDefaults].
func (s *stringList) String() string {
	if s == nil || len(s.values) == 0 {
		return ""
	}

	return fmt.Sprint(s.values)
}

// Set appends one occurrence.
func (s *stringList) Set(value string) error {
	s.values = append(s.values, value)

	return nil
}
