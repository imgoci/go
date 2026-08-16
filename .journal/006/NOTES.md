---
id: 006
title: Spec conformance audit of the Go implementation
started: 2026-08-16
---

## 2026-08-16 16:17 — Kickoff

Goal for the session: run a systematic conformance review of `~/code/imgoci/spec`
against the Go implementation in this repo. Scope is strictly spec-vs-code
validation, not a general code review. Tests that exist to prove spec conformance
are in scope as oracles: a wrong harness means wrong code. Deliverable is a
compiled report.

Current state of the world: `master` is `0b4be41` ("docs: remove plan references
and over-dense comments (#16)"). Sessions 001–004 are closed; 005 (release-readiness
functional test plan) is still in-progress and separately bound. Implementation
covers plan slices 0–6: root `package imgoci` (parse/list/resolve/capabilities/
fetch/fetchfiles/publish), `internal/{jcs,index,filemanifest,file,decomp,transfer,
multipart,registry,retry,auth}`, private `cli/`, conformance fixtures under
`testdata/conformance` pinned to spec commit `5b95710`, plus canonical fixtures and
a CUE cross-check.

Plan: read the spec and map its normative sections to implementation surfaces,
then fan out up to five `conformance` agents over disjoint spec section groups,
each producing rule-by-rule verdicts with file/line evidence over both code and
the tests that claim to prove it. Reconcile findings into a single report,
deduplicate, and rank by severity.
