# Releasing

## Local Gates

Run:

    make fmt
    make test
    make live-test
    go vet ./...

Before a public release, also run:

    git diff
    git status --short

Check for private material with the local secret scanner or an equivalent staged diff scan. Look for absolute local paths, private postcodes/phone numbers, credentials, bearer strings, and real API keys.

Documentation examples must use reserved/example values only.

Also inspect the staged payload before the first public push:

    git diff --cached --stat
    git diff --cached --name-only

Create the public GitHub repo only after the privacy scan is clean:

    gh repo create cavit99/londonjourneycli --public --source=. --remote=origin
    git push -u origin main

## Versioning

Use semantic versions.

- Patch: docs, small bug fixes, alias updates.
- Minor: new commands or manifest fields.
- Major: output contract or exit-code breaks.

## Homebrew

The repo includes a GoReleaser config, but a tap formula is intentionally not shipped in the first cut. Add it once the command surface has survived real use.
