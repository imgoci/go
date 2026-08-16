// Package filemanifest implements the imgoci standard file-manifest codec.
//
// [ValidateStandard] is the consumer half of spec §3.1: grammar decode first,
// then [github.com/imgoci/go/internal/jcs.Verify] on the original bytes, then
// the defined-member checks. Extra members are tolerated wherever spec §3.1
// permits them; they still participate in the canonical-bytes check because
// verification sees the generic JSON tree rather than the semantic model.
package filemanifest
