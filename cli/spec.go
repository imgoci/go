package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	imgoci "github.com/imgoci/go"
)

// publishDocument is the documented JSON publish-spec shape. Every field maps
// onto [imgoci.ReleaseSpec] without loss: paths, selectors, annotations, and
// optional multipart settings.
type publishDocument struct {
	// Name is io.imgoci.name.
	Name string `json:"name"`
	// Version is org.opencontainers.image.version.
	Version string `json:"version"`
	// Annotations are extra root annotations. Keys in the io.imgoci.*
	// namespace are reserved by the library.
	Annotations map[string]string `json:"annotations"`
	// Files are the stored files to publish.
	Files []publishFile `json:"files"`
}

// publishFile is one stored file in a publish document.
type publishFile struct {
	// Path is the filesystem path of the stored file. Relative paths are
	// resolved against the directory that contains the spec.
	Path string `json:"path"`
	// Filename is io.imgoci.filename.
	Filename string `json:"filename"`
	// Architecture is io.imgoci.architecture.
	Architecture string `json:"architecture"`
	// Target is io.imgoci.target.
	Target string `json:"target"`
	// Representation is io.imgoci.representation.
	Representation string `json:"representation"`
	// Role is io.imgoci.role.
	Role string `json:"role"`
	// Compression is io.imgoci.compression. It declares what Path already is.
	Compression string `json:"compression"`
	// Annotations are extra descriptor annotations. Keys in the io.imgoci.*
	// namespace are reserved by the library.
	Annotations map[string]string `json:"annotations"`
	// Multipart requests BigOCI publication. Nil is the standard form.
	Multipart *publishMultipart `json:"multipart"`
}

// publishMultipart selects BigOCI part size. Zero means the library default.
type publishMultipart struct {
	// PartSize is the BigOCI part size in bytes. Zero means the library
	// default (512 MiB). Must not be negative.
	PartSize int64 `json:"partSize"`
}

// loadReleaseSpec reads path as a documented publish-spec JSON document and
// maps it onto [imgoci.ReleaseSpec]. Unknown members are rejected so a typo
// cannot drop a field. Relative file paths are resolved against the directory
// that contains the spec.
func loadReleaseSpec(path string) (imgoci.ReleaseSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return imgoci.ReleaseSpec{}, fmt.Errorf("read publish spec %q: %w", path, err)
	}

	doc, err := decodePublishDocument(raw)
	if err != nil {
		return imgoci.ReleaseSpec{}, fmt.Errorf("parse publish spec %q: %w", path, err)
	}

	return documentToReleaseSpec(doc, filepath.Dir(path))
}

// decodePublishDocument unmarshals raw and rejects unknown members.
func decodePublishDocument(raw []byte) (publishDocument, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var doc publishDocument
	if err := dec.Decode(&doc); err != nil {
		return publishDocument{}, err
	}
	if dec.More() {
		return publishDocument{}, errors.New("trailing data after the JSON value")
	}

	return doc, nil
}

// documentToReleaseSpec maps a decoded document onto [imgoci.ReleaseSpec].
// Required members are checked here so a missing path or selector is a usage
// error before a registry adapter is built.
func documentToReleaseSpec(doc publishDocument, baseDir string) (imgoci.ReleaseSpec, error) {
	if doc.Name == "" {
		return imgoci.ReleaseSpec{}, errors.New("name is required")
	}
	if doc.Version == "" {
		return imgoci.ReleaseSpec{}, errors.New("version is required")
	}
	if len(doc.Files) == 0 {
		return imgoci.ReleaseSpec{}, errors.New("files is required")
	}

	files := make([]imgoci.FileSpec, 0, len(doc.Files))
	for i, file := range doc.Files {
		mapped, err := fileToFileSpec(file, baseDir)
		if err != nil {
			return imgoci.ReleaseSpec{}, fmt.Errorf("files[%d]: %w", i, err)
		}
		files = append(files, mapped)
	}

	return imgoci.ReleaseSpec{
		Name:        doc.Name,
		Version:     doc.Version,
		Annotations: cloneStringMap(doc.Annotations),
		Files:       files,
	}, nil
}

// fileToFileSpec maps one document file onto [imgoci.FileSpec].
func fileToFileSpec(file publishFile, baseDir string) (imgoci.FileSpec, error) {
	if file.Path == "" {
		return imgoci.FileSpec{}, errors.New("path is required")
	}
	if file.Filename == "" {
		return imgoci.FileSpec{}, errors.New("filename is required")
	}
	if file.Architecture == "" {
		return imgoci.FileSpec{}, errors.New("architecture is required")
	}
	if file.Target == "" {
		return imgoci.FileSpec{}, errors.New("target is required")
	}
	if file.Representation == "" {
		return imgoci.FileSpec{}, errors.New("representation is required")
	}
	if file.Role == "" {
		return imgoci.FileSpec{}, errors.New("role is required")
	}
	if file.Compression == "" {
		return imgoci.FileSpec{}, errors.New("compression is required")
	}

	path := file.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}

	spec := imgoci.FileSpec{
		Source: imgoci.FromFile(path),
		Selector: imgoci.Selector{
			Architecture:   file.Architecture,
			Target:         file.Target,
			Representation: file.Representation,
			Role:           file.Role,
			Compression:    file.Compression,
		},
		Filename:    file.Filename,
		Annotations: cloneStringMap(file.Annotations),
	}
	if file.Multipart != nil {
		spec.Multipart = &imgoci.MultipartSpec{PartSize: file.Multipart.PartSize}
	}

	return spec, nil
}

// cloneStringMap returns a shallow copy of m. A nil input stays nil.
func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	maps.Copy(out, m)

	return out
}
