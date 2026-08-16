// Package filemanifest implements the imgoci file-manifest codecs.
//
// [BuildStandard] is the producer half of spec §3.1: the fixed member set, the
// OCI empty-config constant, one application/octet-stream layer, and RFC 8785
// canonical bytes via [github.com/imgoci/go/internal/jcs.Encode].
//
// [ValidateStandard] is the consumer half of spec §3.1: grammar decode first,
// then [github.com/imgoci/go/internal/jcs.Verify] on the original bytes, then
// the defined-member checks. Extra members are tolerated wherever spec §3.1
// permits them; they still participate in the canonical-bytes check because
// verification sees the generic JSON tree rather than the semantic model.
//
// [ValidateBigOCI] is the imgoci BigOCI profile reader: at least two parts,
// io.bigoci.file.{digest,size} extraction, and ASCII-case-insensitive type
// checks via [github.com/imgoci/go/internal/index.EqualMediaType]. A 1-part
// artifact is valid bigoci and invalid imgoci. Extra members the profile does
// not constrain are ignored for imgoci behavior.
package filemanifest
