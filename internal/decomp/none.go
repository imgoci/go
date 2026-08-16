package decomp

import "io"

// noneReader is the identity decoder: stored bytes with no transform.
type noneReader struct {
	// r is the stored-file reader.
	r io.Reader
}

// openNone constructs the identity decoder over r.
func openNone(r io.Reader) (io.ReadCloser, error) {
	return &noneReader{r: r}, nil
}

// Read copies stored bytes unchanged.
func (n *noneReader) Read(p []byte) (int, error) {
	return n.r.Read(p)
}

// Close releases the identity decoder. It does not close the stored-file
// reader; the caller owns that, matching [gzipReader.Close].
func (*noneReader) Close() error {
	return nil
}
