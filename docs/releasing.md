# Releasing

A tag starting with `v` triggers `.github/workflows/release.yml`, which runs the
tests, builds every platform with GoReleaser, publishes a GitHub Release with
checksums, and updates the Homebrew cask.

## One-time setup

### 1. The Homebrew tap

Create a **public** repository named exactly `homebrew-tap` under your account.
It can be empty. The `homebrew-` prefix is what makes `brew tap <owner>/tap`
resolve to it, so the name is not optional.

### 2. A token that can write to it

The default `GITHUB_TOKEN` in a workflow cannot push to a different repository,
so the cask update needs its own token.

Create a fine-grained personal access token with **Contents: read and write**,
scoped to the `homebrew-tap` repository only. Add it to this repository as an
Actions secret named `HOMEBREW_TAP_TOKEN`.

Scope it to that one repository. A token that can write anywhere is a token that
can rewrite this tool's source, and this tool handles SSH keys.

## Before every release

```bash
make check          # vet and race tests
make release-check  # validate .goreleaser.yaml
make snapshot       # build the archives locally, publish nothing
```

`make snapshot` writes to `dist/`. Unpack one and run it, because a broken
archive is only visible at this point:

```bash
tar -tzf dist/ghprofile_*_darwin_arm64.tar.gz
```

## Cutting the release

```bash
git tag -a v0.1.0 -m "First release"
git push origin v0.1.0
```

Then watch the Actions run. The first one is the one likely to fail, because
nothing before it has exercised GoReleaser or the tap token.

## Afterwards

```bash
brew tap <owner>/tap
brew install ghprofile
ghprofile version
```

The version it prints comes from the tag through `-ldflags`, so if it says
`dev`, the build did not go through GoReleaser.

## Getting into homebrew-core

`brew install ghprofile` with no tap means being accepted into homebrew-core, which has
a notability bar: the project has to be maintained, have stable releases, and
show real use, roughly on the order of tens of stars or forks. A new repository
will be turned down.

Until then the tap is the supported route, which is what most Go CLI tools do
indefinitely. If a core formula ever takes the name `ghprofile`, users of the tap
would need `brew install <owner>/tap/ghprofile` to disambiguate.
