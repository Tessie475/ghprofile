# ghprofile

[![CI](https://github.com/Tessie475/ghprofile/actions/workflows/ci.yml/badge.svg)](https://github.com/Tessie475/ghprofile/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Tessie475/ghprofile.svg)](https://pkg.go.dev/github.com/Tessie475/ghprofile)
[![Go Report Card](https://goreportcard.com/badge/github.com/Tessie475/ghprofile)](https://goreportcard.com/report/github.com/Tessie475/ghprofile)

`ghprofile` is a command line tool for using several Git accounts from one machine without per-repository setup.

It reads a declarative profiles file, compares it against the real state of `~/.ssh/config`, `~/.gitconfig` and the key files, and reconciles the difference. Keys, host aliases, commit identities and remotes all derive from that one file.

Separating two accounts by hand is a ten step runbook that is easy to get subtly wrong and impossible to re-run. This replaces it with two commands.

## What It Does

- generates an SSH key per account and walks through putting it on the right one
- writes a `Host` alias per account into `~/.ssh/config`
- scopes a commit identity to a directory with git's `includeIf`, so no repository needs its own config
- reports which account each key actually reaches, rather than whether a connection succeeded
- rewrites existing remotes onto the correct alias
- takes over hand-written stanzas that would shadow its own
- preserves everything outside its markers byte for byte, with a timestamped backup before each overwrite

## Install

```bash
brew install Tessie475/tap/ghprofile
```

<details>
<summary>Install script</summary>

```bash
curl -sSfL https://raw.githubusercontent.com/Tessie475/ghprofile/main/scripts/install.sh | sh
```

[`scripts/install.sh`](scripts/install.sh) selects the right build for the platform, verifies its checksum against the release, and installs to `/usr/local/bin` or `~/.local/bin`.

`GHPROFILE_BIN_DIR` sets the install directory, `GHPROFILE_VERSION` pins a version.

</details>

<details>
<summary>Windows</summary>

```
https://github.com/Tessie475/ghprofile/releases/download/v0.2.7/ghprofile_0.2.7_windows_amd64.zip
```

Unpack it and put `ghprofile.exe` on your `PATH`.

Needs `git` and `ssh-keygen`, both of which [Git for Windows](https://gitforwindows.org) provides.

</details>

<details>
<summary>With Go</summary>

```bash
go install github.com/Tessie475/ghprofile/cmd/ghprofile@latest
```

The binary lands in `$(go env GOPATH)/bin`, which is not on `PATH` by default:

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc && source ~/.zshrc
```

`make build` builds from a checkout.

</details>

Requires `git` and `ssh-keygen`.

## Usage

Declare each identity. The name is an arbitrary label that also forms the SSH alias, so `work` produces `github-work`.

```bash
ghprofile add personal --email you@gmail.com --default
```

```bash
ghprofile add work --email you@company.com --dir ~/company/
```

`add` with no flags prompts instead. The `--default` profile supplies the global git identity; every other profile applies inside the directories it names.

Preview:

```bash
ghprofile apply -dry-run
```

Apply:

```bash
ghprofile apply
```

`apply` generates missing keys, writes the SSH and git configuration, then checks each alias and opens a browser for any key the host does not recognise.

Clone through the alias:

```bash
git clone git@github-work:company/service.git
```

Existing remotes are rewritten in one pass:

```bash
ghprofile fix-remote -all        # preview
ghprofile fix-remote -all -write # apply
```

## Commands

| Command | Description |
|---|---|
| `add <name>` | Add an identity, inferring the alias, key path and commit name |
| `apply` | Take over, generate keys, write config, upload keys, verify |
| `plan` | Print pending changes. `-check` exits 2 when any exist |
| `check` | Inspect the setup offline and report problems |
| `verify` | Ask each host which account its declared key reaches |
| `keys` | Ask which account every key in `~/.ssh` reaches |
| `show` | List declared identities |
| `default <name>` | Choose the identity used where no other matches |
| `remove <name>` | Drop an identity, leaving its key |
| `upload <name>` | Re-run the clipboard and browser handoff |
| `adopt` | Take over hand-written stanzas that shadow a managed one |
| `fix-remote` | Rewrite a repository's remote to the correct alias |
| `init` | Write a starter profiles file |

`check` reads files and opens no connection. `verify` opens a connection and reads no files.

`keys` answers the question `add` cannot: a key's comment is whatever was typed when it was made, so it proves nothing about which account the key reaches. This asks the server.

```
KEY                           HOST        ACCOUNT          PROFILE
~/.ssh/google_cloud_ed25519   github.com  no access        -
~/.ssh/id_ed25519_work        github.com  Chukwu-Eberechi  -
~/.ssh/personal_github        github.com  Tessie475        personal
```

A key already reaching the right account needs no replacement: `ghprofile add <name> -key <path>`.

`ghprofile <command> -h` lists one command's flags.

## Configuration

`~/.config/ghprofile/profiles.yaml` is the desired state. `add` writes it.

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

| Field | Description |
|---|---|
| `host` | the real hostname. GitHub, GitLab, Bitbucket and self-hosted all work |
| `alias` | the SSH `Host` pattern used in place of `host` when cloning |
| `dirs` | directory prefixes where this identity applies, via `gitdir:` |
| `default` | supplies the global identity. At most one profile |
| `options` | extra directives for this profile's `Host` stanza, such as `ProxyJump` |
| `claim_host` | the default profile also answers a plain `git@host:` URL |

`options` exists so that adopting a hand-written stanza does not discard what it carried.

## What It Writes

| Path | Contents |
|---|---|
| `~/.ssh/config` | one `Host` stanza per profile, inside `# BEGIN ghprofile:<name>` markers |
| `~/.gitconfig` | two managed blocks holding `include` and `includeIf` directives |
| `~/.config/ghprofile/gitconfig-<name>` | the commit identity for one profile |
| `~/.ssh/id_ed25519_<name>` | a key, only when the declared path does not exist |

Content outside the markers is preserved byte for byte. Each file is copied to `<path>.ghprofile-backup-<UTC>` once per run before its first overwrite, and every write is atomic.

## Key Passphrases

`ssh-keygen` prompts when `apply` generates a key, as it does when following GitHub's own instructions. An empty answer declines and writes an unencrypted key.

An unencrypted key file is itself the credential: anything able to read it can authenticate. A passphrase makes the file useless on its own. Neither protects an unlocked session, where the agent already holds the key.

Setting one costs a second prompt from `ssh-add`. On macOS the passphrase is then stored in the login keychain and never requested again, because the rendered stanza carries `AddKeysToAgent yes` and `UseKeychain yes`. On Linux expect one prompt per login session.

The agent step is required rather than cosmetic. Verification runs `ssh -o BatchMode=yes` so it cannot hang, and BatchMode also forbids the passphrase prompt, so an encrypted key outside the agent cannot be unlocked.

A passphrase is a new secret, unrelated to any account password, and the host never sees it. There is no reset, but SSH keys carry no history or identity: delete the key, generate another, upload the public half.

`-no-passphrase` skips the prompt. With stdin closed, `ssh-keygen` reads EOF as an empty passphrase, so unattended use works either way.

## Plain URLs

A plain `git@github.com:owner/repo` matches no alias, so SSH offers whatever the agent holds. That usually works and changes after a reboot.

ghprofile does not pin this by default, because pinning restricts every plain connection to that host:

```bash
ghprofile default personal -claim-host
```

The stanza then reads `Host github-personal github.com`, and `IdentitiesOnly yes` means a plain URL offers only that profile's key. Repositories on another account still using plain URLs stop authenticating until `ghprofile fix-remote -all` moves them to their alias. The command explains this and asks first. `-no-claim-host` reverses it.

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success, or no changes pending under `-check` |
| 1 | Failure |
| 2 | `plan -check` only: changes are pending |
| 3 | Bad flags or unknown command |

Code 2 mirrors `terraform plan -detailed-exitcode`, so `ghprofile plan -check` can gate a CI job.

## Out Of Scope

HTTPS token auth, which `gh auth` covers. GPG signing. A TUI. Non-Git SSH hosts. Telemetry.

## Development

```bash
make check   # go vet + go test -race
make lint    # installs the pinned golangci-lint if absent
make help    # list targets
```

`GHPROFILE_HOME` overrides the home directory, which is how the tests drive the CLI against a temporary directory.

`./scripts/sandbox.sh -k` copies the real dotfiles into a temporary directory and runs the whole flow there. Changes should never be tested against live config.

[docs/design-notes.md](docs/design-notes.md) covers why the code is shaped as it is; source files carry one-line comments only. [CONTRIBUTING.md](CONTRIBUTING.md) has the house rules. [docs/releasing.md](docs/releasing.md) covers cutting a release.

## License

MIT
