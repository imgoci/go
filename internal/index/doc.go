// Package index implements the imgoci v1 release-index codec and the ten
// consumer-validation rules in spec §6.
//
// Three seams exist because the spec conformance corpus is parsed-value-only:
// it can accept or reject a decoded index, but it cannot inspect the original
// JSON bytes. Rule 10 (canonical encoding) therefore cannot be decided from a
// [Value] alone.
//
//  1. [Decode] gates UTF-8 with [unicode/utf8.Valid], rejects duplicate object
//     keys at every depth (compared after JSON string decoding), and enforces
//     JSON types for known members. Unknown members are tolerated: spec §5.1
//     and §5.2 require a consumer to accept additional top-level and
//     descriptor members. All annotation keys, including unknown keys, are
//     preserved verbatim.
//  2. [Validate] applies spec §6 rules 1–9 to a decoded [Value]. Each error
//     names the violated rule number. This is the seam the conformance fixtures
//     exercise together with [Decode].
//  3. [VerifyCanonical] applies spec §6 rule 10 to the original bytes by
//     delegating to [github.com/imgoci/go/internal/jcs.Verify]. It must see
//     unknown members, so it verifies a generic JSON tree parsed from the
//     original bytes rather than the semantic [Value].
//
// [Build] is the producer path: it constructs a fixed-shape index from a
// [Model], applies the producer-only registry and annotation-location rules,
// sorts descriptors by the six-field UTF-8 tuple, and encodes RFC
// 8785-canonical bytes with [github.com/imgoci/go/internal/jcs.Encode]. Spec §6
// and §12 require a consumer to accept producer-only violations, so [Validate]
// does not apply those rules.
package index
