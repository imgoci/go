package transfer

import "fmt"

// PublishSource is one stored file a producer intends to upload, as far as
// source validation is concerned.
type PublishSource struct {
	// Path is the path-backed stored file.
	Path string
	// Compression declares what the stored file already is.
	Compression string
	// PartSize is the BigOCI part size in bytes. Meaningful only when
	// Multipart is true.
	PartSize int64
	// Multipart reports whether the caller named a multipart plan.
	Multipart bool
}

// ValidatePublishSources rejects a negative part size, an empty source path,
// and a shared source path whose entries disagree about compression.
func ValidatePublishSources(sources []PublishSource) error {
	byPath := make(map[string]string)
	for i, file := range sources {
		if file.Multipart && file.PartSize < 0 {
			return fmt.Errorf("files[%d]: multipart part size must be >= 0", i)
		}
		path := file.Path
		if path == "" {
			return fmt.Errorf("files[%d]: empty source", i)
		}
		if prev, ok := byPath[path]; ok && prev != file.Compression {
			return fmt.Errorf(
				"shared source %q has conflicting compression %q and %q",
				path, prev, file.Compression,
			)
		}
		byPath[path] = file.Compression
	}
	return nil
}
