# ghprofile

[![CI](https://github.com/Tessie475/ghprofile/actions/workflows/ci.yml/badge.svg)](https://github.com/Tessie475/ghprofile/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Tessie475/ghprofile.svg)](https://pkg.go.dev/github.com/Tessie475/ghprofile)
[![Go Report Card](https://goreportcard.com/badge/github.com/Tessie475/ghprofile)](https://goreportcard.com/report/github.com/Tessie475/ghprofile)

Use two GitHub accounts from one machine, in two commands.

```bash
ghprofile add work --email you@company.com --dir ~/company/
ghprofile apply
```

That generates a key, walks you through putting it on the right account, writes
the SSH host alias, and makes every repository under `~/company/` commit as your
work identity. No per-repository setup, and nothing to remember next time.

---

## The problem

Separating a work and a personal account normally means a ten-step runbook:
generate a key, start the agent, add the key, paste it into GitHub, hand-edit
`~/.ssh/config`, test the connection, clone with the right host alias, fix the
remotes on repositories you already cloned, and set the git identity per
repository.

Every step is a place to get it quietly wrong. Commits land under the wrong
email. A key pasted into the wrong account authenticates anyway, so nothing looks
broken. None of it is re-runnable, so a new laptop means doing it again from
memory.

## Install

### Homebrew

```bash
brew install Tessie475/tap/ghprofile
```

One command. Homebrew taps automatically when you give the full name, so there
is no separate step. Upgrades come through `brew upgrade` like anything else.

Naming the cask in full also satisfies Homebrew's tap trust check, which has
been required since Homebrew 7.0. Taps are repositories of Ruby that `brew`
executes in order to read them, so Homebrew now wants your consent before
loading code from anyone who is not an official tap. Spelling out
`Tessie475/tap/ghprofile` is that consent.

Which means the short form needs trusting the tap first, or it is refused:

```bash
brew trust Tessie475/tap && brew install ghprofile
```

Worth pausing over rather than pasting. That trusts **every** formula and cask
this tap ships, now and in future. If you would rather trust only this one
thing, keep using the full name, or:

```bash
brew trust --cask Tessie475/tap/ghprofile
```

### Download a binary

Every release publishes builds for macOS and Linux on amd64 and arm64, plus
Windows amd64, with a checksums file. Grab yours from
[the releases page](https://github.com/Tessie475/ghprofile/releases), then:

Substitute the version and your platform:

```bash
shasum -a 256 -c ghprofile_<version>_checksums.txt --ignore-missing
```

```bash
tar -xzf ghprofile_<version>_darwin_arm64.tar.gz ghprofile && sudo mv ghprofile /usr/local/bin/
```

<details>
<summary>Script install, for Linux without Homebrew</summary>

[`scripts/install.sh`](scripts/install.sh) picks the right build, verifies it
against the published checksums, and installs it. Download and read it before
you run it:

```bash
curl -sSfLO https://raw.githubusercontent.com/Tessie475/ghprofile/main/scripts/install.sh
```

```bash
less install.sh && sh install.sh
```

`GHPROFILE_BIN_DIR` chooses where it lands, `GHPROFILE_VERSION` pins a version.

It is deliberately not documented as `curl ... | sh`. Piping a remote script
straight into a shell is worth refusing on any tool, and more so on one that
touches your SSH keys.

</details>

<details>
<summary>If you have Go</summary>

```bash
go install github.com/Tessie475/ghprofile/cmd/ghprofile@latest
```

`go install` puts the binary in `$(go env GOPATH)/bin`, which is **not** on your
`PATH` by default. If `ghprofile` comes back as "command not found", that is why:

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc && source ~/.zshrc
```

Or build from a checkout with `make build`.

</details>

| You have | Install with | Taps involved |
|---|---|---|
| Homebrew | `brew install Tessie475/tap/ghprofile` | none, it is automatic |
| neither | a binary from the releases page | none |
| Linux, no Homebrew | `scripts/install.sh` | none |
| Go | `go install ...@latest` | none |

Check it worked, and quote this when reporting a problem:

```bash
ghprofile version
```

Needs `git` and `ssh-keygen`, which you already have if you use SSH with GitHub.
Developed and tested on macOS. Linux should work: the clipboard and browser
helpers have Linux paths but have not been exercised there.

## Getting started

```bash
ghprofile add personal --email you@gmail.com --default
ghprofile add work --email you@company.com --dir ~/company/
ghprofile apply
```

In `add work`, the word `work` is a label you choose. It names the profile and
builds its SSH alias, so `work` gives you `github-work`. Use whatever describes
the account to you: `work`, `personal`, `clientx`, `oss`.

Run `ghprofile add` with no flags and it will ask instead.

The profile marked `--default` supplies your global git identity. Every other
profile applies inside the directories it names, so a repository under
`~/company/` commits as your work identity automatically.

Rehearse before committing to anything:

```bash
ghprofile apply -dry-run
```

### Cloning

Clone through the alias and the right key is used:

```bash
git clone git@github-work:company/service.git
```

Already cloned over HTTPS or a plain URL? Fix them in one pass:

```bash
ghprofile fix-remote -all        # shows what would change
ghprofile fix-remote -all -write # does it
```

## What it does to your machine

Four kinds of thing, and it owns none of your file beyond its own markers:

| File | What it adds |
|---|---|
| `~/.ssh/config` | one `Host` stanza per profile, inside `# BEGIN ghprofile:<name>` markers |
| `~/.gitconfig` | two managed blocks holding `include` and `includeIf` directives |
| `~/.config/ghprofile/gitconfig-<name>` | the commit identity for one profile |
| `~/.ssh/id_ed25519_<name>` | a new key, only when the one you named does not exist |

Everything outside those markers is preserved byte for byte, including comments,
blank lines and indentation. Before overwriting anything it writes a timestamped
`.ghprofile-backup-<UTC>` copy, once per file per run, and every write is atomic,
so an interrupted run cannot leave a half-written config behind.

It never uploads a key without showing you, never asks for a token, and never
touches a `Host` stanza no profile claims.

## Commands

| Command | What it does |
|---|---|
| `add <name>` | Adds an identity, inferring the alias, key path and commit name |
| `apply` | Does everything: take over, keygen, config, key upload, checks. `-no-passphrase` to skip the passphrase prompt |
| `plan` | Shows what `apply` would change. `-check` exits 2 when changes are pending |
| `check` | Inspects your setup offline and reports problems, fixing nothing |
| `verify` | Asks each host which account its key reaches. Needs the network |
| `show` | Lists the identities you have declared |
| `default <name>` | Chooses which identity applies everywhere else |
| `remove <name>` | Drops an identity. Leaves its key alone |
| `upload <name>` | Re-runs just the clipboard and browser handoff |
| `adopt` | Takes over hand-written stanzas that shadow a managed one |
| `fix-remote` | Rewrites a repository's remote to the right alias |
| `init` | Writes a starter profiles file to edit by hand instead |

`check` and `verify` divide the work by where they look. `check` reads your files
and never opens a connection. `verify` opens a connection and never reads your
files. If something is wrong and you do not know which to run, run `check`.

Run `ghprofile <command> -h` for one command's flags.

## The profiles file

`~/.config/ghprofile/profiles.yaml` is the desired state. `add` writes it for
you, but it is plain YAML and yours to edit.

```yaml
version: 1

profiles:
  - name: personal
    host: github.com
    alias: github-personal
    key: ~/.ssh/id_ed25519_personal
    user:
      name: Your Name
      email: you@example.com
    default: true

  - name: work
    host: github.com
    alias: github-work
    key: ~/.ssh/id_ed25519_work
    user:
      name: Your Name
      email: you@company.com
    dirs:
      - ~/company/
    options:
      ServerAliveInterval: "60"
```

`options` adds extra directives to that profile's `Host` stanza. Use it to carry
across anything you had hand-written, such as keepalives or a `ProxyJump`, so
that adopting a stanza does not quietly lose settings. The directives ghprofile
always writes cannot be overridden this way.

`host` is not assumed to be GitHub. GitLab, Bitbucket and self-hosted work the
same way:

```yaml
  - name: clientx
    host: gitlab.example.com
    alias: gitlab-clientx
    key: ~/.ssh/id_ed25519_clientx
    user:
      name: Your Name
      email: you@clientx.com
    dirs:
      - ~/clients/x/
```

## Key passphrases

When `apply` generates a key, `ssh-keygen` asks you for a passphrase, exactly as
it does when you follow GitHub's own instructions:

```
Enter passphrase for "/Users/you/.ssh/id_ed25519_work" (empty for no passphrase):
Enter same passphrase again:
```

**Press Enter twice to decline.** The key is then written unencrypted, which is
what most people do and is a reasonable choice on a machine you trust.

### What declining costs you

Without a passphrase the key file *is* the credential. Anything that can read
`~/.ssh/id_ed25519_work` can push as you: a backup on an unencrypted drive, a
home directory that got synced somewhere, a package's install script, a stolen
laptop. With one, the file alone is useless.

It protects a file at rest. It does not protect an unlocked session, where the
agent already holds the key.

### What accepting costs you

Almost nothing on macOS. If you set a passphrase, ghprofile runs `ssh-add
--apple-use-keychain` straight afterwards, so you are asked once more and then
macOS stores it in your login keychain. Every `git push` after that is silent,
including across reboots. On Linux there is no keychain equivalent, so expect a
prompt once per login session.

That second step is not cosmetic. Verification runs `ssh -o BatchMode=yes` so it
can never hang, but BatchMode also forbids the passphrase prompt, so a key that
is encrypted and not in the agent would make the check fail and report a working
setup as broken.

### Forgetting it

There is no reset, but very little is at stake. The passphrase is probably in
your login keychain already: open Keychain Access and search for the key's path.
And if it is not, keys are access tokens rather than identity. Nothing is tied
to them, no repositories, no commits, no history. Delete the key, generate
another, upload the new public half, remove the old one. Five minutes.

The passphrase is a new secret you invent at the prompt. It is not your GitHub
password, and GitHub never sees it or knows it exists. It unlocks a local file,
nothing more.

### Scripts

```bash
ghprofile apply -no-passphrase
```

With stdin closed, `ssh-keygen` reads EOF as an empty passphrase and carries on
rather than hanging, so CI works either way. The flag is for saying so on
purpose instead of relying on that.

## Plain URLs

A plain `git clone git@github.com:owner/repo.git` matches no alias, so SSH offers
whatever key your agent happens to hold. That usually works, and silently changes
after a reboot.

ghprofile does **not** fix this by default, because fixing it means restricting
every plain connection to that host. If you want it pinned:

```bash
ghprofile default personal -claim-host
```

The stanza then reads `Host github-personal github.com`, and because it carries
`IdentitiesOnly yes`, a plain URL offers **only** that profile's key.
Repositories on a different account still using plain URLs will stop
authenticating until you point them at that account's alias, which
`ghprofile fix-remote -all` does. The command explains that and asks before
changing anything.

`-no-claim-host` reverses it. `check` mentions the unpinned state as information,
never as a problem to fix.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success, or no changes pending under `-check` |
| 1 | Something went wrong |
| 2 | `plan -check` only: changes are pending |
| 3 | Bad flags or unknown command |

Code 2 mirrors `terraform plan -detailed-exitcode`, so `ghprofile plan -check`
can gate a CI job asserting that a machine is configured.

## Out of scope

HTTPS token auth, which is `gh auth`'s job. GPG signing. A TUI. Managing non-Git
SSH hosts. Telemetry of any kind.

## Development

```bash
make check   # go vet + go test -race
make test
make lint    # needs golangci-lint
make help    # list targets
```

`GHPROFILE_HOME` overrides the home directory, which is how the tests drive the
CLI against a temporary directory without touching the real one.

Never test a change against your own dotfiles. Use the sandbox, which copies them
somewhere temporary and runs the whole flow there:

```bash
./scripts/sandbox.sh -k
```

- [docs/design-notes.md](docs/design-notes.md) explains why the code is shaped
  the way it is. Source files carry one-line comments only, so anything needing a
  paragraph lives there
- [CONTRIBUTING.md](CONTRIBUTING.md) has the house rules
- [docs/releasing.md](docs/releasing.md) covers cutting a release and the tap setup

## License

[MIT](LICENSE)
