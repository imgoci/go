package imgoci_test

import (
	"fmt"

	imgoci "github.com/imgoci/go"
)

// canonicalMinimal is the RFC 8785 encoding of the spec minimal pass fixture.
const canonicalMinimal = `{"annotations":{"io.imgoci.name":"example","org.opencontainers.image.version":"1"},"artifactType":"application/vnd.imgoci.release.v1","manifests":[{"annotations":{"io.imgoci.architecture":"amd64","io.imgoci.compression":"none","io.imgoci.content.digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","io.imgoci.content.size":"0","io.imgoci.filename":"a","io.imgoci.representation":"x-test-format","io.imgoci.role":"x-test-file","io.imgoci.target":"x-test-target"},"artifactType":"application/vnd.imgoci.file.v1","digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111","mediaType":"application/vnd.oci.image.manifest.v1+json","size":1}],"mediaType":"application/vnd.oci.image.index.v1+json","schemaVersion":2}`

func ExampleParseIndex() {
	idx, err := imgoci.ParseIndex([]byte(canonicalMinimal))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(idx.Name())
	fmt.Println(idx.Version())
	// Output:
	// example
	// 1
}

func ExampleIndex_Resolve() {
	idx, err := imgoci.ParseIndex([]byte(canonicalMinimal))
	if err != nil {
		fmt.Println(err)
		return
	}
	sel, err := idx.Resolve(imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "x-test-target",
		Representation: "x-test-format",
		Compressions:   []string{"none"},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sel.Entries()[0].Selector.Role)
	fmt.Println(sel.Entries()[0].Filename)
	// Output:
	// x-test-file
	// a
}
