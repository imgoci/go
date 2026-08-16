---
title: Use Docker credentials
description: Authenticate library and CLI transfers with the credentials docker login stores, or with a static credential.
---

# Use Docker credentials

This guide shows how to make imgoci transfers authenticate with the credentials `docker login` stores, how the CLI applies them without being asked, and how to run anonymously or with a static credential instead.

Prerequisites:

- an account on the registry, if it requires one
- the Docker CLI for the `docker login` route, or a registry token for the static route

## Log in with Docker

```sh
docker login ghcr.io
```

`docker login` writes its configuration to `$DOCKER_CONFIG/config.json` when that variable is set, and to `.docker/config.json` under your home directory otherwise (`$HOME` on Unix, `%USERPROFILE%` on Windows). That file holds either credential entries directly or the name of a credential helper program that stores them elsewhere. imgoci reads the same file from the same locations, and only ever reads — no transfer writes a credential anywhere.

## Opt in from the library

Docker credentials are opt-in for library callers. Name the option when building the client:

```go
client, err := imgoci.New(imgoci.WithDockerCredentials())
if err != nil {
	// A configuration file that exists but cannot be read as a Docker
	// configuration fails here, at New, not in the middle of a transfer.
	return err
}
```

The opt-in is deliberate: a configuration that names a credential helper turns a lookup into running someone else's program, and a library that did that merely because it was linked in would be a surprise with a security dimension. Naming `WithDockerCredentials` is the consent for both the file read and the helper execution.

What to expect:

- **No configuration file, or no home directory and no `$DOCKER_CONFIG`:** not an error. That is a machine nobody has logged in on; every registry resolves to the anonymous credential.
- **A file that exists but cannot be read as a Docker configuration:** `New` returns an error. That covers a read failure and a parse failure. Failing quietly would hide a credential you meant to be used and surface as a confusing authorization failure later.
- **The file is read once at `New`.** A `docker login` run during a transfer is not picked up by an existing client; build a new one.

Anonymous is not "authentication off": a registry that demands a token still gets the full token exchange, made anonymously, which is what public-read registries expect.

## The CLI applies them always

The private reference CLI has no credential flag. Docker credentials are always on: log in with `docker login`, then run the tool — the model every other registry CLI's users already know.

```sh
docker login ghcr.io
imgoci list ghcr.io/example/os:v1
```

To force an anonymous run — for example, to check what an unauthenticated consumer would see — point `DOCKER_CONFIG` at an empty directory. A missing configuration file resolves every registry anonymously:

```sh
DOCKER_CONFIG=$(mktemp -d) imgoci list ghcr.io/example/os:v1
```

An authorization refusal exits with code `4` (`imgoci.ErrUnauthorized`); see the [CLI reference](../reference/cli.md).

## Credential helpers

When the configuration names a credential helper (`credsStore` or `credHelpers`), the lookup runs that program — for example `docker-credential-osxkeychain`. Helpers are asked afresh at every lookup, and each execution is capped at 10 seconds so a wedged helper fails the transfer instead of hanging it forever. The cap is a constant, not a knob; a transfer's overall deadline belongs to your `context`.

A helper is only run when the file in front of the client names one. An empty or absent configuration never falls through to the platform's default helper, so a lookup against an empty `DOCKER_CONFIG` directory cannot reach your real keychain.

One stored shape is refused rather than degraded: a credential stored as an identity token (some single-sign-on flows produce these) is an error, because this client cannot exchange one and silently proceeding would downgrade the transfer to anonymous.

## Docker Hub's legacy key

A Docker Hub login is stored under the legacy key `https://index.docker.io/v1/`, while a reference names the registry `docker.io` — or spells out the dialed host `registry-1.docker.io`. The lookup maps both spellings onto the stored key, so a plain `docker login` works for Docker Hub references without any configuration edits. No other registry name is rewritten, and the lookup key is always the registry your reference named — never a name a registry offered in a challenge.

## Use a static credential instead

When you already hold the secret — a CI job with a registry token in its environment, a program with its own configuration — skip the Docker file entirely:

```go
client, err := imgoci.New(imgoci.WithCredentials("ci-bot", os.Getenv("REGISTRY_TOKEN")))
```

Nothing is looked up, no file is read, and no helper runs. The secret is a password or, at most registries today, a personal access token. Unlike the Docker route, which answers only for hosts a login was stored under, `WithCredentials` presents the credential to whatever host a reference names — you chose both the secret and the reference, so you decide who sees it.

Naming both credential options on one `New` call leaves the last one named in effect.

## Related pages

- [Publish and fetch your first release](../tutorials/first-release.md) — a local registry needs no credentials at all.
- [API reference](../reference/api.md) — `New`, `WithDockerCredentials`, `WithCredentials`.

Implemented spec revision: imgoci v1 draft, 2026-08-11 ([imgoci/spec](https://github.com/imgoci/spec) commit `5b957102eeda16498fdcb80a738431b83abd4197`).
