package index

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/opencontainers/go-digest"
)

const (
	// reservedAnnotationPrefix is the spec-reserved annotation namespace.
	reservedAnnotationPrefix = "io.imgoci."
)

// ProducerInput is a producer's release input before any bytes are hashed.
type ProducerInput struct {
	// Name is io.imgoci.name.
	Name string
	// Version is org.opencontainers.image.version.
	Version string
	// Annotations are extra root annotations. Keys in the io.imgoci.*
	// namespace are reserved and rejected.
	Annotations map[string]string
	// Files are the stored files a producer intends to publish.
	Files []ProducerFile
}

// ProducerFile is one stored file a producer intends to publish.
type ProducerFile struct {
	// Selector is the six-field identity.
	Selector Selector
	// Filename is io.imgoci.filename.
	Filename string
	// Annotations are extra descriptor annotations. Keys in the io.imgoci.*
	// namespace are reserved and rejected.
	Annotations map[string]string
}

// ValidateProducerFields checks the caller-supplied strings: non-empty name and
// version, UTF-8 everywhere, and no reserved io.imgoci.* annotation.
func ValidateProducerFields(in *ProducerInput) error {
	if in.Name == "" {
		return errors.New("name is empty")
	}
	if in.Version == "" {
		return errors.New("version is empty")
	}
	if err := checkUTF8Spec(in); err != nil {
		return err
	}
	if err := checkReservedAnnotations(in); err != nil {
		return err
	}
	return nil
}

// ValidateProducerRules runs the index rules over placeholder content identity.
//
// It runs [Build] on a placeholder model so selector grammar, required roles,
// duplicate six-field tuples, incus-vm→incus, filename collisions, and rule 6's
// filename agreement surface as [ErrRule] rather than a retrieved-index
// [Validate] failure.
//
// Placeholder content digest and size are identical for every entry that
// shares (architecture, target, representation, usage, role). That lets
// [Build] enforce rule 6's filename component without pretending to
// know content identity. Real content digest and size are checked after
// pass-1 hashing, before any network write.
func ValidateProducerRules(in *ProducerInput) error {
	entries := make([]ModelEntry, len(in.Files))
	for i, file := range in.Files {
		identityKey := placeholderIdentityKey(file.Selector)
		entries[i] = ModelEntry{
			Digest:        digest.FromBytes([]byte("manifest:" + strconv.Itoa(i))),
			Size:          1,
			Selector:      file.Selector,
			ContentDigest: digest.FromBytes([]byte("content:" + identityKey)),
			ContentSize:   0,
			Filename:      file.Filename,
			Annotations:   file.Annotations,
		}
	}
	_, err := Build(&Model{
		Name:        in.Name,
		Version:     in.Version,
		Annotations: in.Annotations,
		Entries:     entries,
	})
	if err != nil {
		return err
	}
	return nil
}

// checkUTF8Spec requires [utf8.ValidString] on every caller string.
func checkUTF8Spec(in *ProducerInput) error {
	if err := requireUTF8("name", in.Name); err != nil {
		return err
	}
	if err := requireUTF8("version", in.Version); err != nil {
		return err
	}
	if err := checkUTF8Map("root annotation", in.Annotations); err != nil {
		return err
	}
	for i, file := range in.Files {
		prefix := fmt.Sprintf("files[%d]", i)
		if err := requireUTF8(prefix+" filename", file.Filename); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" architecture", file.Selector.Architecture); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" target", file.Selector.Target); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" representation", file.Selector.Representation); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" role", file.Selector.Role); err != nil {
			return err
		}
		if err := requireUTF8(prefix+" compression", file.Selector.Compression); err != nil {
			return err
		}
		if err := checkUTF8Map(prefix+" annotation", file.Annotations); err != nil {
			return err
		}
	}
	return nil
}

// checkUTF8Map requires UTF-8 keys and values.
func checkUTF8Map(label string, m map[string]string) error {
	for k, v := range m {
		if err := requireUTF8(label+" key", k); err != nil {
			return err
		}
		if err := requireUTF8(label+" value", v); err != nil {
			return err
		}
	}
	return nil
}

// checkReservedAnnotations rejects io.imgoci.* keys in caller maps.
func checkReservedAnnotations(in *ProducerInput) error {
	for k := range in.Annotations {
		if strings.HasPrefix(k, reservedAnnotationPrefix) {
			return fmt.Errorf("reserved annotation %q", k)
		}
	}
	for i, file := range in.Files {
		for k := range file.Annotations {
			if strings.HasPrefix(k, reservedAnnotationPrefix) {
				return fmt.Errorf("files[%d] reserved annotation %q", i, k)
			}
		}
	}
	return nil
}

// placeholderIdentityKey is the spec §2 file key used to share placeholder
// content identity across transport alternatives during [ValidateProducerRules].
// Fields are joined with "/" so a comma-separated usage set cannot collide
// with a neighboring field.
func placeholderIdentityKey(s Selector) string {
	return strings.Join([]string{
		s.Architecture,
		s.Target,
		s.Representation,
		s.Usage,
		s.Role,
	}, "/")
}
