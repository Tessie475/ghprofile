# Contributing

Bug reports and patches are welcome.

## Before you open a pull request

```bash
make check        # go vet + go test -race
gofmt -l .        # must print nothing
golangci-lint run # if you have it installed
```

## Working on it safely

This tool rewrites `~/.ssh/config` and `~/.gitconfig`, so never test a change
against your own files. Use the sandbox, which copies them into a temporary
directory and never writes to your real home:

```bash
./scripts/sandbox.sh -k
```

Tests drive the CLI through `GHPROFILE_HOME`, which overrides the home
directory. If you add a test that could reach the network or the real home,
it is wrong: see `TestApply_NoUploadStaysOffline` for why.

## What the code expects of you

- One-line comments only. Anything needing a paragraph goes in
  [docs/design-notes.md](docs/design-notes.md)
- Logic that decides things stays pure and testable. `internal/plan` performs
  no IO at all, and that is deliberate
- Nothing outside a `# BEGIN ghprofile:` marker may ever be modified. There is
  a round-trip test for this and it is the project's most important invariant
- Every write goes through `apply.WriteAtomic`, and every overwrite through
  `apply.Backups`

## Reporting a bug

Include the output of:

```bash
ghprofile check
ghprofile plan
```

Both are read-only. Redact anything you would rather not share: they print
paths and email addresses, never key material.
