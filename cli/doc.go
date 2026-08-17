// Command imgoci publishes, lists, resolves, and fetches imgoci releases.
//
// This is a private reference command line interface: verification tooling,
// never published, never released, never versioned. It exists so a human can
// watch the library work against a real registry. Nothing in the repository
// publishes it, and the local replace directive in its go.mod makes installing
// it from a module proxy impossible on purpose.
//
// It is a thin wrapper. Transfer and selection flags map onto public library
// options or query fields. -timeout is not a library option: it is the CLI
// command-context deadline. There is no transfer logic here, no retry, resume,
// or authentication logic, and no interface of its own. Argument dispatch uses
// the standard library [flag] package only.
//
// # Commands
//
//	imgoci publish [flags] <spec> <ref>
//	imgoci list    [flags] <ref>
//	imgoci resolve [flags] <ref>
//	imgoci fetch   [flags] <ref> <dest>
//	imgoci help    [publish|list|resolve|fetch|version]
//	imgoci version
//
// A flag that was not set passes nothing to the library, so the library's own
// default applies and the CLI never restates it.
//
// Docker credentials are always on. There is no flag for them. Log in with
// `docker login`, then run the tool. A test that needs a run with no
// credentials points DOCKER_CONFIG at an empty directory. -plain-http talks
// http:// to a local registry.
//
// Incomplete or ambiguous operands are rejected before a registry adapter is
// built. Publish is tag-only. Resolve and fetch require -architecture,
// -target, -representation, and at least one -compression. Unset -usage on
// resolve and fetch selects the empty usage set.
//
// # Publish spec
//
// <spec> is a JSON document that maps losslessly onto [imgoci.ReleaseSpec].
// Unknown members are rejected so a typo cannot drop a field. Relative file
// paths are resolved against the directory that contains <spec>.
//
//	{
//	  "name": "example",
//	  "version": "1",
//	  "annotations": {"note": "root"},
//	  "files": [
//	    {
//	      "path": "disk.qcow2",
//	      "filename": "disk.qcow2",
//	      "architecture": "amd64",
//	      "target": "qemu",
//	      "representation": "qcow2",
//	      "usage": ["install-offline", "install"],
//	      "role": "disk",
//	      "compression": "none",
//	      "annotations": {"note": "file"},
//	    }
//	  ]
//	}
//
// name, version, and files are required. name must be a basic token: 1 to 128
// ASCII bytes matching ^[a-z0-9]+([._-][a-z0-9]+)*$. version must contain 1
// to 128 printable ASCII characters and no whitespace or control characters.
// Each file requires path, filename, and the five selector fields. filename is
// 1–255 bytes, ASCII alphanumeric first and last, with ASCII alphanumerics plus
// ".", "_", "+", "-" internally.
// usage is optional: omitted, null, and [] are the empty set. Order is
// irrelevant; the CLI sorts, de-duplicates, and rejects install-offline
// without install. annotations may be omitted. multipart omitted or null
// selects the standard form; a present object requests BigOCI publication.
// partSize must not be negative; 0 uses the library default (512 MiB) as the
// effective part size.
// A multipart plan must satisfy ceil(storedSize/effectivePartSize) <= 4096.
//
// # Output contract
//
// Standard output carries machine data only. publish writes the canonical
// index digest and a newline, and writes nothing when it fails. list and
// resolve write deterministic tab-separated listings. fetch writes nothing
// either way. Help and version asked for by name go to standard output and
// exit zero.
//
// list prints one line per stored transport alternative, in the order
// [imgoci.Index.List] already sorts:
//
//	<architecture>\t<target>\t<representation>\t<usage>\t<role>\t<compression>\t<artifactType>
//
// resolve prints one line per selected role, in [imgoci.Resolved.Entries]
// order:
//
//	<architecture>\t<target>\t<representation>\t<usage>\t<role>\t<compression>\t<filename>\t<artifactType>\t<contentDigest>\t<contentSize>
//
// An empty match prints nothing. Diagnostics, progress, failure summaries,
// and usage complaints go to standard error, each prefixed "imgoci: ".
// Usage blocks are unprefixed. There is no terminal detection, no color, and
// no line rewriting, so the output is byte-identical piped and interactive.
//
// # Exit codes
//
//	0    success
//	1    failure, no sentinel matched
//	2    usage error
//	3    errors.Is(err, imgoci.ErrNotFound)
//	4    errors.Is(err, imgoci.ErrUnauthorized)
//	5    errors.Is(err, imgoci.ErrInvalidIndex)
//	6    errors.Is(err, imgoci.ErrInvalidSpec)
//	7    errors.Is(err, imgoci.ErrInvalidDest)
//	8    errors.Is(err, imgoci.ErrDigestMismatch)
//	9    errors.Is(err, imgoci.ErrUnsupportedType)
//	10   errors.Is(err, imgoci.ErrSelectionMismatch)
//	11   errors.Is(err, imgoci.ErrDecode)
//	130  interrupted by SIGINT
//	143  terminated by SIGTERM
//
// A failure always prints two lines. The first preserves the library's graphic
// error text without re-wrapping or re-phrasing it, and visibly escapes every
// non-graphic rune so peer-controlled detail cannot create another log record
// or terminal control. The second is unconditional and takes one of three
// forms: the sentinel [errors.Is] matched and the code it maps to, the
// statement that none matched, or the signal that stopped the run, written
// "interrupted by SIGINT (exit 130)" or "terminated by SIGTERM (exit 143)".
//
// A recorded signal outranks the error's shape. A usage error is the
// exception: it prints its prefixed complaint and then the offending
// command's unprefixed usage block, and exits 2.
package main
