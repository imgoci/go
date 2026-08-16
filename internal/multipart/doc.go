// Package multipart is the BigOCI adapter for the transfer Multipart port.
//
// It wraps the public [github.com/imgoci/bigoci.Client] and maps imgoci
// settings onto bigoci options: [github.com/imgoci/bigoci.WithPlainHTTP],
// static [github.com/imgoci/bigoci.WithCredentials], and
// [github.com/imgoci/bigoci.WithHTTPClient] only when a client is injected.
// A nil HTTP client leaves bigoci to build its own default verified stack
// (ARCHITECTURE.md §6.6.3). The unverified external-transport option is
// never used.
//
// [Client.Push] calls [github.com/imgoci/bigoci.Client.PushByDigest] against a
// repository-only reference; no tag is written. [Client.PullTo] pulls by
// digest onto a path. Resume semantics are bigoci's: a failed or interrupted
// pull leaves a sibling partial file beside the destination
// (`path` + ".bigoci-partial").
//
// The adapter owns its retry budget — bigoci's internal loop. internal/retry
// must never wrap these calls (ARCHITECTURE.md §6.5). A non-nil report
// callback converts each bigoci snapshot into that transfer's
// latest-absolute wire bytes and retries. Concurrent transfers are merged
// by the orchestrator, never here. A nil report skips conversion.
package multipart
