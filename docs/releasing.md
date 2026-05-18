# Releasing

## Local Gates

Run:

    make fmt
    make test
    make live-test
    go vet ./...
    londonjourneycli --skills-dir ./examples/skills lint
    londonjourneycli --skills-dir ./examples/skills doctor tfl-journey

If this checkout has Clawpatch state, revalidate before merging:

    clawpatch revalidate --all
    clawpatch status --json

Before a public release, also run:

    git diff
    git status --short

Check for private material with the local secret scanner or an equivalent staged diff scan. Look for absolute local paths, private postcodes/phone numbers, credentials, bearer strings, and real API keys.

Documentation examples must use reserved/example values only.

Inspect the staged payload before pushing:

    git diff --cached --stat
    git diff --cached --name-only

This repository is already public at github.com/cavit99/londonjourneycli. Push only after the privacy scan and gates are clean:

    git push

## Versioning

Use semantic versions.

- Patch: docs, small bug fixes, alias updates.
- Minor: new commands or manifest fields.
- Major: output contract or exit-code breaks.

Release tags use v-prefixed semver:

    git tag -a v0.3.2 -m "v0.3.2"
    git push origin v0.3.2

## Homebrew

The repo includes a GoReleaser config for darwin/linux amd64/arm64 archives and checksums. A tap formula is intentionally not shipped here; add it once the command surface has survived more real use.
