package imgoci

import "errors"

// Sentinel errors are the public error surface declared in ARCHITECTURE.md
// section 3.3. Network-only sentinels stay inert until slice 2.
var (
	// ErrNotFound reports that a registry does not hold the requested release,
	// manifest, or blob.
	//
	// This sentinel is declared as part of the stable public error surface. It
	// is inert until the network consumer lands in slice 2; offline helpers
	// never return it.
	ErrNotFound = errors.New("not found")

	// ErrUnauthorized reports that a registry refused a request for lack of
	// credentials or insufficient permission.
	//
	// This sentinel is declared as part of the stable public error surface. It
	// is inert until the network consumer lands in slice 2; offline helpers
	// never return it.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrInvalidIndex reports invalid retrieved imgoci documents: the release
	// index (spec §6) and file manifests (spec §3.1). The wrapped error names
	// the failed decode, structural, canonical-bytes, or identity check.
	ErrInvalidIndex = errors.New("invalid index")

	// ErrInvalidSpec reports a producer-side specification violation, including
	// an illegal publish reference form.
	ErrInvalidSpec = errors.New("invalid spec")

	// ErrInvalidDest reports that a fetch destination plan failed preflight.
	//
	// This sentinel is declared as part of the stable public error surface. It
	// is inert until the network consumer lands in slice 2; offline helpers
	// never return it.
	ErrInvalidDest = errors.New("invalid destination")

	// ErrDigestMismatch reports that retrieved or published bytes did not match
	// a declared digest or size, including a Source that changed between pass 1
	// and upload.
	//
	// This sentinel is declared as part of the stable public error surface.
	ErrDigestMismatch = errors.New("digest mismatch")

	// ErrUnsupportedType reports that a selected file-manifest type is outside
	// the consumer capability set. Offline [Index.Resolve] uses it when
	// capability filtering leaves a selected role with no remaining transport
	// alternative.
	ErrUnsupportedType = errors.New("unsupported type")

	// ErrSelectionMismatch reports that a [Resolved] value was not derived from
	// the release being retrieved. Binding is by canonical index digest, not
	// pointer identity.
	//
	// This sentinel is declared as part of the stable public error surface. It
	// is inert until the network consumer lands in slice 2; offline helpers
	// never return it.
	ErrSelectionMismatch = errors.New("selection mismatch")

	// ErrDecode reports that strict decompression of a stored file failed.
	// The producer path is as strict as the consumer: a two-member gzip fails
	// before any upload.
	ErrDecode = errors.New("decode")
)
