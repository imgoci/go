package transfer

import (
	"errors"

	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/file"
)

// Fault is the category of a failure, as far as the orchestrator and its
// adapters can classify it.
type Fault int

const (
	// FaultNone is a nil error.
	FaultNone Fault = iota
	// FaultUnknown is nothing this package recognizes.
	FaultUnknown
	// FaultCommit is a [*file.CommitError].
	FaultCommit
	// FaultInvalidPlan is [file.ErrInvalidPlan].
	FaultInvalidPlan
	// FaultInvalidDocument is [ErrInvalidDocument].
	FaultInvalidDocument
	// FaultDigestMismatch is [decomp.ErrSizeExceeded], [decomp.ErrSizeMismatch],
	// or [ErrDigestMismatch].
	FaultDigestMismatch
	// FaultDecode is [decomp.ErrDecode].
	FaultDecode
	// FaultNotFound is [ErrNotFound].
	FaultNotFound
	// FaultUnauthorized is [ErrUnauthorized].
	FaultUnauthorized
)

// Classify reports which fault category err belongs to.
//
// BigOCI profile violations arrive as [ErrInvalidDocument]
// (retrieved-document rule, same as a standard-form manifest failure) and
// stored digest/size mismatches as [ErrDigestMismatch]. A nil
// Multipart wiring error is not a sentinel.
//
// [decomp.ErrSizeExceeded] (a stored file longer than the layer descriptor
// declares) and [decomp.ErrSizeMismatch] (one shorter) are integrity failures,
// not decode failures, and are matched ahead of [decomp.ErrDecode] so a size
// verdict is not reported as a codec verdict.
func Classify(err error) Fault {
	if err == nil {
		return FaultNone
	}

	var commit *file.CommitError
	if errors.As(err, &commit) {
		return FaultCommit
	}

	switch {
	case errors.Is(err, file.ErrInvalidPlan):
		return FaultInvalidPlan
	case errors.Is(err, ErrInvalidDocument):
		return FaultInvalidDocument
	case errors.Is(err, decomp.ErrSizeExceeded),
		errors.Is(err, decomp.ErrSizeMismatch),
		errors.Is(err, ErrDigestMismatch):
		return FaultDigestMismatch
	case errors.Is(err, decomp.ErrDecode):
		return FaultDecode
	case errors.Is(err, ErrNotFound):
		return FaultNotFound
	case errors.Is(err, ErrUnauthorized):
		return FaultUnauthorized
	default:
		return FaultUnknown
	}
}

// CommitFault reports the roles committed before a commit failure, the role
// that failed, and whether err is a commit failure at all.
func CommitFault(err error) ([]string, string, bool) {
	var commit *file.CommitError
	if !errors.As(err, &commit) {
		return nil, "", false
	}

	return commit.Committed, commit.Role, true
}
