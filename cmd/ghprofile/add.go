package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Tessie475/ghprofile/internal/apply"
	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/keys"
	"github.com/Tessie475/ghprofile/internal/shell"
	"github.com/Tessie475/ghprofile/internal/sshconfig"
)

const addUsage = `Usage: ghprofile add <name> [flags]

<name> is yours to invent. It is a short label for one identity, used in the
profiles file and to build the ssh alias, so "work" becomes github-work. Pick
whatever describes the account to you: work, personal, clientx, oss.

Examples:
  ghprofile add personal --email you@gmail.com --default
  ghprofile add work     --email you@company.com --dir ~/company/
  ghprofile add clientx  --email you@clientx.com --dir ~/clients/x/ --host gitlab.com

Run it with no flags and it will ask you instead.

Flags:
`

// ErrProfileExists is returned when adding a name that is already declared.
var ErrProfileExists = errors.New("profile already exists")

func (a *app) cmdAdd(ctx context.Context, args []string) int {
	fset := a.flags("add")
	fset.Usage = func() {
		fmt.Fprint(a.errOut, addUsage)
		fset.PrintDefaults()
	}
	email := fset.String("email", "", "commit email for this identity (required)")
	name := fset.String("name", "", "commit name (defaults to your global git user.name)")
	host := fset.String("host", "github.com", "git host")
	alias := fset.String("alias", "", "ssh host alias (defaults to <provider>-<profile>)")
	key := fset.String("key", "", "private key path (defaults to ~/.ssh/id_ed25519_<profile>)")
	dir := fset.String("dir", "", "directory this identity applies to")
	makeDefault := fset.Bool("default", false, "use this identity everywhere no other profile matches")
	claimHost := fset.Bool("claim-host", false, "also make plain git@<host> URLs use this identity (default only, read the warning)")
	assumeYes := fset.Bool("yes", false, "do not prompt to confirm -claim-host")
	positional, err := parse(fset, args)
	if err != nil {
		return exitMisuse
	}

	f, loadErr := config.LoadOrNew(a.paths.ProfilesFile, a.home)
	if loadErr != nil {
		return a.fail(loadErr)
	}

	in := bufio.NewReader(a.in)

	profile := first(positional)
	if profile == "" {
		profile = a.ask(in, "A short name for this identity, your choice (e.g. work, personal, clientx)", "")
	}
	if profile == "" {
		fset.Usage()
		return exitMisuse
	}
	if _, exists := f.ByName(profile); exists {
		return a.fail(fmt.Errorf("%q: %w", profile, ErrProfileExists))
	}

	if *email == "" {
		*email = a.ask(in, fmt.Sprintf("Commit email for %q", profile), "")
	}
	if *name == "" {
		*name = a.ask(in, "Commit name", a.gitUserName(ctx))
	}
	if *alias == "" {
		*alias = defaultAlias(*host, profile)
	}
	if *key == "" {
		*key = a.suggestKey(f, *alias, profile)
	}
	if *dir == "" && !*makeDefault {
		*dir = a.ask(in, fmt.Sprintf("Directory where %q applies (blank to make it your default identity)", profile), "")
	}

	p := config.Profile{
		Name:    profile,
		Host:    *host,
		Alias:   *alias,
		Key:     *key,
		User:    config.User{Name: *name, Email: *email},
		Default: *makeDefault,
	}
	if *dir != "" {
		p.Dirs = []string{*dir}
	}
	// The first profile has to be the default, or nothing governs your global
	// identity.
	if len(f.Profiles) == 0 || *makeDefault {
		for i := range f.Profiles {
			f.Profiles[i].Default = false
		}
		p.Default = true
	}
	if p.Options == nil {
		p.Options = a.inheritedOptions(*alias)
	}

	if *claimHost {
		switch {
		case !p.Default:
			fmt.Fprintln(a.errOut, "ghprofile: -claim-host only applies to the default profile, ignoring it")
		case a.confirmClaim(p.Name, p.Alias, p.Host, *assumeYes):
			p.ClaimHost = true
		}
	}

	f.Version = config.Version
	f.Profiles = append(f.Profiles, p)

	if err := f.ExpandPaths(a.home); err != nil {
		return a.fail(err)
	}
	if err := f.Validate(); err != nil {
		return a.fail(err)
	}

	data, err := f.Marshal(a.home)
	if err != nil {
		return a.fail(err)
	}
	if err := os.MkdirAll(a.paths.ConfigDir, 0o700); err != nil {
		return a.fail(err)
	}
	if _, err := apply.NewBackups().Save(a.paths.ProfilesFile); err != nil {
		return a.fail(err)
	}
	if err := apply.WriteAtomic(a.paths.ProfilesFile, data, 0o600); err != nil {
		return a.fail(err)
	}

	fmt.Fprintf(a.out, "\nadded %q to %s\n\n", profile, a.paths.ProfilesFile)
	fmt.Fprintf(a.out, "  alias     git@%s\n", p.Alias)
	fmt.Fprintf(a.out, "  key       %s\n", p.Key)
	fmt.Fprintf(a.out, "  identity  %s <%s>\n", p.User.Name, p.User.Email)
	if len(p.Dirs) > 0 {
		fmt.Fprintf(a.out, "  applies   %s\n", strings.Join(p.Dirs, ", "))
	} else {
		fmt.Fprintln(a.out, "  applies   everywhere no other profile matches")
	}
	a.noteExistingKeys(p.Key)
	fmt.Fprintln(a.out, "\nnext: ghprofile apply")
	return exitOK
}

// noteExistingKeys points out the keys already on the machine when the profile
// is about to generate a new one.
//
// Reusing a key is only inferred when a hand-written stanza names the same
// alias, which misses the common case: one key, no ssh config, plain URLs. The
// result was that someone with a working setup was told to generate and upload
// a second key, with nothing saying -key existed.
func (a *app) noteExistingKeys(chosen string) {
	if _, err := os.Stat(chosen); err == nil {
		return
	}

	existing, err := keys.Existing(a.paths.SSHDir)
	if err != nil || len(existing) == 0 {
		return
	}

	fmt.Fprintf(a.out, "\n  %s does not exist yet and apply will generate it.\n", chosen)
	fmt.Fprintf(a.out, "  there %s already %d %s in %s.\n",
		plural(len(existing), "is", "are"), len(existing),
		plural(len(existing), "keypair", "keypairs"), a.paths.SSHDir)
	// Deliberately not listing them with their comments. A comment is whatever
	// was typed at keygen time and proves nothing about which account a key
	// reaches; "keys" asks the server instead.
	fmt.Fprintln(a.out, "  to see which account each one reaches:  ghprofile keys")
	fmt.Fprintln(a.out, "  to reuse one:                           ghprofile add <name> -key <path>")
}

// ask prompts for a value, offering a default the user can accept with Enter.
func (a *app) ask(in *bufio.Reader, question, fallback string) string {
	if fallback != "" {
		fmt.Fprintf(a.out, "%s [%s]: ", question, fallback)
	} else {
		fmt.Fprintf(a.out, "%s: ", question)
	}

	line, _ := in.ReadString('\n')
	if answer := strings.TrimSpace(line); answer != "" {
		return answer
	}
	return fallback
}

// gitUserName reads your existing global commit name, so the prompt has a
// sensible default rather than an empty box.
func (a *app) gitUserName(ctx context.Context) string {
	res, err := shell.Run(ctx, "git", "config", "--global", "user.name")
	if err != nil || res.Code != 0 {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// suggestKey reuses the key a hand-written stanza for this exact alias already
// points at, so adopting an account you set up by hand does not generate a
// second key for it.
//
// It matches on the alias and never on the hostname. Several profiles share a
// hostname by definition, so a hostname match would hand the same key to every
// account on that host, which is the one thing this tool exists to prevent.
func (a *app) suggestKey(f *config.Profiles, alias, profile string) string {
	fallback := "~/.ssh/id_ed25519_" + profile

	data, err := os.ReadFile(a.paths.SSHConfig)
	if err != nil {
		return fallback
	}

	for _, s := range sshconfig.Stanzas(string(data)) {
		if s.Managed || s.IdentityFile == "" {
			continue
		}
		for _, h := range s.Hosts {
			if !strings.EqualFold(h, alias) {
				continue
			}
			if keyTaken(f, s.IdentityFile, a.home) {
				return fallback
			}
			return s.IdentityFile
		}
	}
	return fallback
}

// keyTaken reports whether another profile already uses this key.
func keyTaken(f *config.Profiles, key, home string) bool {
	expanded := key
	if trimmed, ok := strings.CutPrefix(key, "~/"); ok {
		expanded = home + "/" + trimmed
	}
	for _, p := range f.Profiles {
		if p.Key == expanded || p.Key == key {
			return true
		}
	}
	return false
}

// inheritedOptions carries across directives from a hand-written stanza with
// the same alias, which adopt would otherwise discard.
func (a *app) inheritedOptions(alias string) map[string]string {
	data, err := os.ReadFile(a.paths.SSHConfig)
	if err != nil {
		return nil
	}
	for _, s := range sshconfig.Stanzas(string(data)) {
		if s.Managed {
			continue
		}
		for _, h := range s.Hosts {
			if strings.EqualFold(h, alias) && len(s.Options) > 0 {
				return s.Options
			}
		}
	}
	return nil
}

// defaultAlias names the alias after the provider, so github-work reads
// obviously and does not collide with a real hostname.
func defaultAlias(host, profile string) string {
	lower := strings.ToLower(host)
	switch {
	case strings.Contains(lower, "github"):
		return "github-" + profile
	case strings.Contains(lower, "gitlab"):
		return "gitlab-" + profile
	case strings.Contains(lower, "bitbucket"):
		return "bitbucket-" + profile
	}

	for _, label := range strings.Split(lower, ".") {
		switch label {
		case "git", "www", "ssh", "com", "org", "net", "io":
			continue
		default:
			return label + "-" + profile
		}
	}
	return profile + "-git"
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
