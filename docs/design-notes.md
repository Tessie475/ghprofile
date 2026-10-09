# Design notes

Why the code is shaped the way it is. The source files keep only one-line comments, so anything that needs a paragraph lives here.

---

## `internal/config`

### Parsing is separate from loading

`Parse` takes `[]byte`. It never opens a file. Reading from disk happens in `Load` (step 6).

The reason is testability. A test for `Parse` needs a string literal, so thirty cases cost nothing. A test for something that opens files needs a temporary directory per case, which is slow, noisy, and fails for reasons unrelated to parsing.

The general shape, used throughout this project: push input and output to the edges, keep the logic in the middle pure. Module 2's block engine works on `[]byte` and never opens a file. Module 6's planner is a pure function over desired and actual state.

### Parsing is separate from validating

`Parse` decodes and refuses malformed input. It does not judge the contents. `version: 99` with blank names is a structurally fine YAML document, so it comes back with no error, and deciding it is unacceptable belongs to `Validate`.

Mixing the two would make both harder to test, because every parse test would need semantically valid data and every validation test would need syntactically valid YAML.

### Unknown fields are rejected

`yaml.Unmarshal` ignores keys with no matching struct field, which turns a typo like `emial:` into a silently missing setting. `yaml.NewDecoder` with `KnownFields(true)` makes it an error instead.

That is the right default for a config file, where a silently ignored setting is worse than a loud failure.

### Empty input arrives as `io.EOF`

When the input contains no YAML document at all, `Decode` returns `io.EOF` rather than a syntax error or a zero value. That covers an empty file, a whitespace-only file, and a comment-only file in one check, because none of them contain a document.

Note that a **tab** in otherwise-blank input is different: tabs are illegal for YAML indentation, so that produces a syntax error, not `io.EOF`.

### Errors

Three patterns, used everywhere in this package:

- **Sentinels** declared at package level (`ErrEmpty`, `ErrMissingField`, ...) so callers can recognise a condition with `errors.Is` instead of matching on message text, which breaks the moment the wording changes.
- **Wrapping** with `fmt.Errorf("...: %w", err)`, which adds context while keeping the cause reachable. `%v` would flatten it to text and sever the chain.
- **Joining** with `errors.Join(errs...)` when several things are wrong at once, so a user can fix everything in one pass instead of one round trip per problem.

`errors.Is` walks the whole chain of wrapped errors, which is why `errors.Is(err, io.EOF)` is correct and `err == io.EOF` is a latent bug.

---

## `cmd/ghprofile`

### Exit codes are a contract

Once documented, scripts and CI depend on them, so they cannot change:

| Code | Meaning |
|---|---|
| 0 | Success. In `--check` mode, no changes pending |
| 1 | Something went wrong |
| 2 | `--check` mode only: changes are pending |
| 3 | Bad flags or unknown subcommand |

Code 2 mirrors `terraform plan -detailed-exitcode`, so `ghprofile plan --check` can gate a CI job.

### `main` calls `run`

`main` is a single `os.Exit(run(...))`. All the real work is in `run`, which returns an `int` instead of exiting.

That is what makes the CLI testable. A test can call `run([]string{"plan"})` and assert on the code. It cannot do that with a function that calls `os.Exit`, because that kills the test binary.

---

## Testing conventions

### `t.Fatal` versus `t.Error`

`t.Errorf` records a failure and keeps going, so one run reports every broken assertion. `t.Fatalf` stops the test immediately.

Use `Error` by default. Use `Fatal` only when continuing would be meaningless or would panic, for example after a length check that later assertions index past.

Never use `log.Fatal` or `os.Exit` in a test. They kill the whole test binary: other tests are skipped, cleanup never runs, and the framework cannot report what failed.

### `wantErr` holds an error, not a bool

In the table-driven tests, `wantErr` is an `error`:

- `nil` means "any error is acceptable, we only care that it was rejected"
- a sentinel means "specifically this one", checked with `errors.Is`

A bool would only tell you that something failed, not that it failed for the reason you intended.

### The `goodX()` plus `mutate` pattern

Validation tests start from a helper returning a known-good value, then apply a one-line `mutate` function that breaks exactly one thing. Each case stays a single line and it is obvious what is under test.

### `testdata/`

The directory name is special to the Go toolchain: it is ignored when building, so fixtures never end up compiled into the binary. `go test` runs from the package directory, so the relative path `testdata/profiles.yaml` works.

---

## `internal/blocks`

### Markers, and why the tool owns so little

Everything ghprofile writes into a file you also edit lives between a matching
pair of markers:

```
# BEGIN ghprofile:work
...
# END ghprofile:work
```

`Upsert` replaces the body of a named block in place, or appends the block when
it is absent. `Remove` deletes one. Everything outside the markers is preserved
byte for byte, including comments, blank lines and indentation.

This is the most important correctness property in the project, because the
files involved are ones a human wrote by hand and cannot easily reconstruct.

### The blank-line symmetry

`Upsert` always writes one blank separator line before an appended block, and
`Remove` always absorbs one blank line above the block it removes.

Those two rules have to agree. An earlier version only added the separator when
the previous line was not already blank, which meant that upserting into a file
already ending in a blank line, then removing, silently ate one. The round-trip
test caught it.

---

## `internal/sshconfig` and `internal/gitconfig`

Both render text and hand it to `blocks`. Neither opens a file.

`sshconfig` writes one block per profile. `gitconfig` writes a single block
named `identities`, holding an unconditional `[include]` for the default profile
followed by one `[includeIf "gitdir:..."]` per directory. Git applies the last
matching include, so a directory-specific identity correctly overrides the
global one.

### Shadowing

OpenSSH uses the **first** `Host` stanza that matches, not the last. So a
hand-written `Host github-work` earlier in the file makes the managed block
below it dead. `Shadowed` finds those, `check` reports them, and `adopt`
removes the hand-written stanza so the managed block takes effect.

`adopt` deliberately leaves behind any comment lines above the stanza it
removes. The tool does not delete lines it did not write.

---

## `internal/plan` and `internal/apply`

`plan.Compute` is pure: given a desired document and a snapshot of the machine,
it returns an ordered list of actions. It performs no IO, reads no clock and
consults no environment, so the entire decision logic is testable with no
filesystem at all, and `-dry-run` is simply not executing the result.

`plan.ReadState` is the only part of that package that touches the disk.

`apply` is the only package permitted to write. Writes go to a temporary file in
the same directory, are chmodded and synced, then renamed over the target.
Rename within a filesystem is atomic, so a crash mid-write leaves the original
intact.

---

## `internal/verify`

GitHub exits **1** on a successful `ssh -T`, so the exit code is not the signal.
The signal is the greeting on stderr:

```
Hi your-username! You've successfully authenticated, but GitHub does not provide shell access.
```

`ParseGreeting` pulls the account name out of that. Knowing which account
answered is the whole value: it is what catches a key pasted into the wrong
GitHub account, which an exit code alone can never tell you.

`verify` also flags two aliases that report the same account, which means one
key is not being used at all.

### Per-profile options

`adopt` removes a hand-written stanza so the managed block can take effect, which
means any directive in that stanza and not in the managed block is lost. Real
use turned this up immediately: a hand-written `Host github-work` carried
`ServerAliveInterval` and `ServerAliveCountMax`, and neither survived.

The `options` map carries them across. Keys are rendered in sorted order, which
matters more than it looks: Go randomises map iteration, so an unsorted render
would produce a different stanza on every run and `apply` would never converge.
There is a test that renders fifty times and insists the output never changes.

### Backups are taken once per run, not once per write

A single `apply` can write the same file twice: once to take over a hand-written
stanza, once to add the managed block. Both happen within the same second.

The first version copied the file before every write and named the copy with
second precision. The second copy therefore captured an already-modified file
and, sharing a name, overwrote the first. The only record of the user's original
state was destroyed by the command that was supposed to preserve it. A live test
caught it: a 22-line config left behind a 16-line backup.

`Backups` now records which paths it has copied during a run and refuses to copy
one twice, so the backup always holds the pre-run state. `reserve` also appends a
counter when a name is taken, so two runs inside the same second cannot clobber
each other. Both cases have regression tests, the second using an injected clock
frozen to a single instant.

### Claiming the bare hostname is opt-in

A plain `git@github.com:owner/repo` URL matches no alias, so ssh falls back to
offering every identity it can find, including whatever the agent has loaded.
Since the managed stanzas set `AddKeysToAgent yes`, this usually authenticates
as whichever account you used most recently. It works by accident and changes
after a reboot.

The default profile **can** list the hostname as a second pattern, but only when
`claim_host: true` is set, which `ghprofile default <name> -claim-host` does
after printing a warning and asking:

```
Host github-personal github.com
```

One stanza, two patterns, both resolving to the same `HostName`. Only the
default may claim it, so nothing else can match a plain URL, and
`IdentitiesOnly yes` means exactly one key is offered.

It is off by default because the blast radius is wrong relative to the benefit.
It takes ownership of every plain connection to that host in order to fix a
case the tool already steers users away from: if you are using aliases, and
setting up aliases is the tool's whole job, you are not using plain URLs. A
stranger running `apply` for the first time could find repositories that worked
yesterday failing with `Permission denied (publickey)` and no visible
connection to the command they ran. `check` reports the unpinned state as
information instead.

`Shadowed` counts the claimed hostname as owned, so a hand-written `Host
github.com` is reported by `check` and removed by `adopt` like any other
shadowing stanza.

Two caveats worth knowing:

- A bare URL now offers **only** the default key. Repositories on another
  account using bare URLs must move to that account's alias, which
  `fix-remote` does.
- `Apply` writes the default's block first, but `blocks.Upsert` replaces an
  existing block in place, so on a re-apply the ordering is whatever it was.
  That is harmless here because exactly one stanza can ever match the bare
  hostname, so first-match has nothing to choose between. The ordering is
  defensive, not load-bearing.

### A default profile keeps its directories

An earlier version cleared `Dirs` when a profile became the default, on the
reasoning that a default applies everywhere so directories are redundant. They
are redundant, but discarding them lost information the user had typed, and
made `ghprofile default` destructive. Both the unconditional include and the
conditional ones are now written, which is harmless and lossless.

### check, not doctor

The command was called `doctor`, following `brew doctor` and `flutter doctor`.
The problem was not that it was cute, it was that it collided with `verify`:
both read as "check my setup" and a user could not guess which to run.

`check` and `verify` now split on where they look. `check` reads files and never
opens a connection. `verify` opens a connection and never reads files. That is a
line you can state in one sentence, which `doctor` versus `verify` was not.

`doctor` still dispatches to `check` but is left out of the help text, so anyone
reaching for the convention lands somewhere sensible without the two names
competing in the documentation.

### Passphrases are opt-in, and never an argument

`ssh-keygen -N ""` writes an unencrypted key, which is the default because the
tool exists to make multi-account work unattended, and a key that prompts on
every use breaks that for anyone without a keychain.

`-passphrase` drops the `-N` and runs `ssh-keygen` through
`shell.RunInteractive`, which attaches the terminal so ssh-keygen prompts and
confirms the passphrase itself.

There is deliberately no `-passphrase=<value>` form. Arguments are visible in
the process list for the lifetime of the command and, if a human typed them, in
shell history indefinitely. A flag that looks convenient and leaks the secret
protecting a private key is worse than no flag.

On macOS the cost of opting in is close to zero, because the stanzas already
carry `AddKeysToAgent yes` and `UseKeychain yes`, so the passphrase is stored in
the login keychain after first use. That asymmetry is why the flag exists rather
than just a paragraph in the README saying the default is fine.

### verify takes its ssh, clock and sleep as fields

`SSH` and `Wait` used to call `shell.Run` and `time.Now` directly, which left no
seam and no tests. Both sat at 0% coverage in the one package whose job is to
tell a user whether their setup actually works. The worst failure this tool can
have is confidently reporting a broken setup as fine, and nothing proved it did
not.

`Checker` holds the ssh invocation, the clock and the sleep as fields. The
package-level `SSH` and `Wait` still exist and call `New()`, so no caller
changed. Tests build a `Checker` whose runner replays canned `shell.Result`
values and whose sleep advances a fake clock instead of waiting, which makes the
retry tests instant while still exercising the real budget arithmetic.

What that bought, specifically:

- **Exit code 1 with a greeting is success.** GitHub exits 1 on a successful
  `ssh -T`, so treating the code as the signal would report every working setup
  as broken. That is now asserted, as is exit 1 *without* a greeting being a
  failure.
- The backoff doubles and caps at eight seconds.
- The budget bounds the number of attempts.
- Cancellation stops the loop.
- A missing ssh binary fails immediately rather than being retried, since it
  will not fix itself.
- `BatchMode=yes` is still passed, which is what stops ssh blocking on a
  passphrase prompt inside `apply`.

### An encrypted key has to go into the agent immediately

`-passphrase` was shipped generating an encrypted key and nothing more, which
was wrong. Verification runs `ssh -o BatchMode=yes`, deliberately, so that
`apply` can never hang on a prompt. But BatchMode also forbids the passphrase
prompt, so a freshly encrypted key cannot be unlocked and the check that runs
seconds later reports a perfectly good setup as broken.

Measured, to be sure of the mechanism rather than assuming it:

    plaintext key                  usable with no secret at all
    encrypted key                  unusable without the passphrase
    encrypted key, in the agent    usable with no prompt

So `apply -passphrase` now runs `ssh-add` after generating, with
`--apple-use-keychain` on darwin. That costs a second prompt once, and on macOS
the keychain means never again.

`AddKeysToAgent yes` in the rendered stanza does not help here. It adds a key to
the agent *after* a successful use, and the first use is the thing that cannot
happen.

### ssh-keygen asks, the way GitHub's instructions do

The first version passed `-N ""`, which suppresses the prompt and writes an
unencrypted key. A later version added `-passphrase` to opt in.

Both were wrong, for the same reason. GitHub's documented command,
`ssh-keygen -t ed25519 -C "you@example.com"`, always asks, and an empty answer
declines. Measured rather than assumed:

    Enter passphrase for "github_way" (empty for no passphrase): Enter same passphrase again:
      resulting cipher: none

So GitHub does not give you a passphrase by default. It gives you the *question*
by default, and most people press Enter. `-N ""` removed the question, which
meant a user never learned the choice existed. An opt-in flag had the same
effect, since nobody reads a flag list before running a command.

`Generate` now omits `-N` and runs through `shell.RunInteractive`, so the prompt
appears. `-no-passphrase` exists for scripts that want to be explicit, though
with stdin closed ssh-keygen already reads EOF as an empty passphrase and
carries on rather than hanging.

One consequence: the command cannot know what the user typed. So `apply` reads
the answer off the file with `keys.IsEncrypted`, which parses the cipher name
out of the OpenSSH private key header, and only runs `ssh-add` when the key is
actually encrypted. Guessing would either skip the agent for a key that needs it
or add a keychain entry for one that does not.

`privateKeyCipher` validates its lengths before indexing, and compares as int64
on both sides. Subtracting before converting would underflow on a truncated
file and let a bogus length through, which gosec caught.

### The cask postflight hook is load-bearing

The cask clears `com.apple.quarantine` in a `postflight` block. v0.2.2 removed
it and v0.2.3 put it back, because removing it broke installation:

    "ghprofile" Not Opened
    Apple could not verify "ghprofile" is free of malware that may harm your Mac

with Move to Trash as the default button.

The reasoning for removing it was that quarantine is applied in
`Cask::Artifact::Moved`, which backs `app`, `pkg` and `font`, while a `binary`
artifact is `Binary < Symlinked < Relocated` and never touches it. Only
`moved.rb` mentions quarantine in `cask/artifact/`.

That was true and irrelevant. Homebrew propagates the attribute from the
downloaded archive to the staged files, and that code is not in
`cask/artifact/`. The conclusion came from grepping one directory and treating
absence there as absence everywhere. The lesson is narrower than "check more
directories": an inference about whether something is needed is not evidence,
and the only proof available was an install, which was not done.

Homebrew deprecates `postflight` in favour of `postflight_steps`, so every
install prints a warning telling the user to file a bug against the tap. That
stays, because `postflight_steps` takes a structured sandboxed step list rather
than a Ruby block and GoReleaser 2.18 has no field that emits one. A noisy
warning is clearly the lesser problem next to a binary that will not start.

The real fix is signing and notarizing the binary, which removes quarantine's
bite entirely and needs a paid Apple Developer account.

### IsEncrypted understands three key formats

`privateKeyCipher` only read the `openssh-key-v1` magic, so a legacy RSA key
returned `ErrMalformedPrivateKey`. Nothing could reach that path, since the only
caller reads a key ghprofile generated seconds earlier, but the function is
exported and telling someone their working key is malformed is the wrong answer.

The formats now classified, each from information kept in clear text:

| Armor | How encryption is read |
|---|---|
| `BEGIN OPENSSH PRIVATE KEY` | length-prefixed cipher name in the decoded body |
| `BEGIN ENCRYPTED PRIVATE KEY` | PKCS#8 says so in the armor itself |
| `BEGIN PRIVATE KEY` | PKCS#8, unencrypted by definition |
| `BEGIN RSA/DSA/EC PRIVATE KEY` | traditional PEM, a `Proc-Type: 4,ENCRYPTED` header above the base64 |

Anything else returns `ErrUnsupportedKeyFormat`, which is distinct from
`ErrMalformedPrivateKey` so a caller can say something true. The default is an
error rather than "not encrypted", because claiming a key is unencrypted when it
cannot be read would skip the agent step and make verification fail for a reason
the user could not act on.

### Git config paths use forward slashes

`gitconfig` writes every path through `gitPath`, which converts separators on
Windows. Writing `filepath.Join`'s output raw produced:

    [include]
        path = C:\Users\GOKU\.config\ghprofile\gitconfig-personal

Git treats a backslash as an escape character inside a value, so `\U` and `\G`
are invalid escapes and git rejects the **whole file**:

    fatal: bad config line 3 in file C:/Users/GOKU/.gitconfig

Every git command on that machine failed until the managed blocks were deleted
by hand. Worse than the tool not working, since the tool had already finished.

The same applied to `gitdir:` patterns, which came out doubly wrong:
backslashes from `filepath.Join` and a trailing forward slash from
`withTrailingSlash`.

Git's own writer escapes them instead, which is visible in any existing Windows
config as `C:\\Users\\you`. Forward slashes avoid the question and git
accepts them everywhere, including in `gitdir:`.

`toGitPath` takes the platform as an argument rather than reading
`runtime.GOOS` directly, so the conversion is testable from a Mac. Converting
unconditionally would be wrong: a backslash is a legal character in a Unix
filename.

This shipped in v0.2.4 and was found by the first person to run ghprofile on
Windows. Nothing in the test suite could have caught it, because `filepath`
behaves correctly per platform and every test ran on macOS.

### keys asks the server, because a comment proves nothing

`add` reuses an existing key only when a hand-written stanza names the same
alias. That misses the common case: one key, no ssh config, plain URLs. Someone
with a working setup was told to generate and upload a second key, with nothing
saying `-key` existed.

The first fix listed the keypairs in `~/.ssh` with their comments. That was
worse than useless on a real machine:

    ~/.ssh/google_cloud_ed25519  (echukwu@chisquares.com)

A comment is a label typed at `ssh-keygen` time. That key is a Google Cloud key
carrying a work email and has no GitHub access at all. Listing it beside a work
profile suggestion implies a relationship that does not exist.

`ghprofile keys` asks instead, through `verify.Checker.Key`, which names the key
with `-i` and sets `IdentitiesOnly=yes` and `IdentityAgent=none`. The agent
exclusion matters: with an agent loaded, ssh would offer its keys too and the
greeting could name an account the key under test has nothing to do with.

`add` now reports only the count and points at `keys`, rather than vouching for
a filename it cannot vouch for.

v0.2.5 shipped this fix incomplete. The default-identity block, which is line 3
of the file and therefore the exact line git rejected, still wrote the raw path.
The edit to it had matched nothing because of an indentation difference, and
the script reported success anyway.

The test meant to catch it only asserted when `runtime.GOOS == "windows"`, so it
could never fail on macOS or in CI, which is everywhere tests run. `isWindows`
is now a package variable the test sets, so the Windows branch is exercised on
every platform, and the test was confirmed to fail against the v0.2.5 code
before being trusted.
