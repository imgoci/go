package registry

import (
	"errors"
	"mime"
	"strings"
)

// parseContentType returns the parameter-free media type from a Content-Type
// header.
//
// Valid RFC 9110 parameters (charset, boundary, and anything else) are
// stripped. The bare type is what the root package's EqualMediaType compares.
// A missing or malformed header is an error; the peer-controlled value is
// not copied into the message. [mime.ParseMediaType] accepts a type with no
// subtype ("application"); that is not a media type, so it is refused here.
func parseContentType(header string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(header)
	typ, sub, found := strings.Cut(mediaType, "/")
	if err != nil || !found || typ == "" || sub == "" {
		return "", errors.New("content type is not a valid media type")
	}

	return mediaType, nil
}
