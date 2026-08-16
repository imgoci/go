package multipart

import (
	"github.com/imgoci/bigoci"
)

// convertProgress adapts a latest-absolute (wireBytes, retries) callback to a
// bigoci callback. A nil report stays nil so an unwatched transfer does no
// conversion.
func convertProgress(report func(wireBytes int64, retries int)) bigoci.ProgressFunc {
	if report == nil {
		return nil
	}

	return func(p bigoci.Progress) {
		report(p.WireBytes, p.Retries)
	}
}
