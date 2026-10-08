package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Tessie475/ghprofile/internal/apply"
	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/plan"
	"github.com/Tessie475/ghprofile/internal/sshconfig"
	"github.com/Tessie475/ghprofile/internal/verify"
)

const starter = `version: 1

# Each profile is one identity. "ghprofile add" writes these for you, so this
# file is only worth editing by hand for something the flags do not cover.
#
# profiles:
#   - name: personal
#     host: github.com
#     alias: github-personal
#     key: ~/.ssh/id_ed25519_personal
#     user:
#       name: Your Name
#       email: you@example.com
#     default: true
#
#   - name: work
#     host: github.com
#     alias: github-work
#     key: ~/.ssh/id_ed25519_work
#     user:
#       name: Your Name
#       email: you@company.com
#     dirs:
#       - ~/work/
`

// parse handles flags that appear after positional arguments. Go's flag
// package stops at the first non-flag word, so "add work --email x" would
// otherwise leave --email unparsed and silently fall back to prompting.
func parse(fset *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args

	for {
		if err := fset.Parse(rest); err != nil {
			return nil, err
		}
		if fset.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fset.Arg(0))
		rest = fset.Args()[1:]
	}
}

func (a *app) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	return fs
}

func (a *app) load() (*config.Profiles, int) {
	f, err := config.Load(a.paths.ProfilesFile, a.home)
	if err == nil {
		return f, exitOK
	}
	// An empty file reads the same way as a missing one to someone who has not
	// declared anything yet, so both get the same advice.
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, config.ErrNoProfiles) {
		fmt.Fprintf(a.errOut, "ghprofile: no identities declared in %s\n", a.paths.ProfilesFile)
		fmt.Fprintln(a.errOut, "run: ghprofile add personal --email you@example.com --default")
		return nil, exitError
	}
	return nil, a.fail(err)
}

func (a *app) cmdInit(args []string) int {
	fset := a.flags("init")
	force := fset.Bool("force", false, "overwrite an existing profiles file")
	if err := fset.Parse(args); err != nil {
		return exitMisuse
	}

	if _, err := os.Stat(a.paths.ProfilesFile); err == nil && !*force {
		fmt.Fprintf(a.errOut, "ghprofile: %s already exists, use -force to replace it\n", a.paths.ProfilesFile)
		return exitError
	}

	if err := os.MkdirAll(a.paths.ConfigDir, 0o700); err != nil {
		return a.fail(err)
	}
	if err := apply.WriteAtomic(a.paths.ProfilesFile, []byte(starter), 0o600); err != nil {
		return a.fail(err)
	}

	fmt.Fprintf(a.out, "wrote %s\n", a.paths.ProfilesFile)
	fmt.Fprintln(a.out, "edit it, then run: ghprofile plan")
	return exitOK
}

func (a *app) cmdShow(args []string) int {
	if err := a.flags("show").Parse(args); err != nil {
		return exitMisuse
	}
	f, code := a.load()
	if f == nil {
		return code
	}

	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tALIAS\tHOST\tIDENTITY\tDIRECTORIES\tDEFAULT")
	for _, p := range f.Profiles {
		dirs := strings.Join(p.Dirs, ", ")
		if dirs == "" {
			dirs = "-"
		}
		def := ""
		if p.Default {
			def = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.Alias, p.Host, p.User.Email, dirs, def)
	}
	return flushOr(w, a)
}

func flushOr(w *tabwriter.Writer, a *app) int {
	if err := w.Flush(); err != nil {
		return a.fail(err)
	}
	return exitOK
}

func (a *app) cmdPlan(args []string) int {
	fset := a.flags("plan")
	check := fset.Bool("check", false, "exit 2 when changes are pending")
	if err := fset.Parse(args); err != nil {
		return exitMisuse
	}

	actions, code := a.computePlan()
	if actions == nil {
		return code
	}

	if len(actions) == 0 {
		fmt.Fprintln(a.out, "up to date, nothing to do")
		return exitOK
	}

	for _, act := range actions {
		fmt.Fprintf(a.out, "%s\n    %s\n", act, act.Reason)
		if d := act.Diff(); d != "" {
			fmt.Fprint(a.out, d)
		}
	}
	fmt.Fprintf(a.out, "\n%d change(s) pending. run: ghprofile apply\n", len(actions))

	if *check {
		return exitDrift
	}
	return exitOK
}

func (a *app) computePlan() ([]plan.Action, int) {
	f, code := a.load()
	if f == nil {
		return nil, code
	}

	st, err := plan.ReadState(f, a.paths)
	if err != nil {
		return nil, a.fail(err)
	}

	actions, err := plan.Compute(f, st, a.paths, a.darwin)
	if err != nil {
		return nil, a.fail(err)
	}
	if actions == nil {
		actions = []plan.Action{}
	}
	return actions, exitOK
}

func (a *app) cmdApply(ctx context.Context, args []string) int {
	fset := a.flags("apply")
	dry := fset.Bool("dry-run", false, "print everything that would happen, change nothing")
	noBackup := fset.Bool("no-backup", false, "do not write timestamped backups before overwriting")
	noAdopt := fset.Bool("no-adopt", false, "leave hand-written stanzas in place, even when they shadow a profile")
	noUpload := fset.Bool("no-upload", false, "do not offer to put missing keys on their accounts")
	noPassphrase := fset.Bool("no-passphrase", false, "do not ask for a passphrase when generating a key, write it unencrypted")
	noBrowser := fset.Bool("no-browser", false, "do not open a browser when uploading")
	timeout := fset.Duration("timeout", 90*time.Second, "how long to wait for a key to become live")
	if err := fset.Parse(args); err != nil {
		return exitMisuse
	}

	f, code := a.load()
	if f == nil {
		return code
	}

	backups := apply.NewBackups()
	if *noBackup {
		backups = nil
	}

	if !*noAdopt {
		if code := a.takeOver(f, *dry, backups); code != exitOK {
			return code
		}
	}

	actions, code := a.computePlan()
	if actions == nil {
		return code
	}

	if len(actions) > 0 {
		opts := apply.Options{DryRun: *dry, Backups: backups, NoPassphrase: *noPassphrase, UseKeychain: a.darwin}
		if err := apply.Run(ctx, actions, opts, a.out); err != nil {
			return a.fail(err)
		}
		fmt.Fprintln(a.out)
	} else {
		fmt.Fprintln(a.out, "configuration already up to date")
	}

	if *dry {
		if len(actions) > 0 {
			fmt.Fprintf(a.out, "%d change(s) not applied (dry run)\n", len(actions))
		}
		return exitOK
	}

	if *noUpload {
		fmt.Fprintln(a.out, "run ghprofile verify to check each account")
		return exitOK
	}

	return a.settleAccounts(ctx, f, uploadOpts{noBrowser: *noBrowser, timeout: *timeout})
}

// takeOver removes hand-written stanzas that would shadow a managed block.
// OpenSSH honours the first matching Host, so leaving them in place would make
// everything else this command does invisible.
func (a *app) takeOver(f *config.Profiles, dry bool, backups *apply.Backups) int {
	data, err := os.ReadFile(a.paths.SSHConfig)
	if errors.Is(err, fs.ErrNotExist) {
		return exitOK
	}
	if err != nil {
		return a.fail(err)
	}

	content := string(data)
	shadowed := sshconfig.Shadowed(content, f)
	if len(shadowed) == 0 {
		return exitOK
	}

	next := content
	for _, alias := range shadowed {
		next = sshconfig.RemoveHost(next, alias)
	}

	for _, alias := range shadowed {
		fmt.Fprintf(a.out, "take over          Host %s (was hand-written and would win)\n", alias)
	}
	if dry {
		return exitOK
	}

	if _, err := backups.Save(a.paths.SSHConfig); err != nil {
		return a.fail(err)
	}
	if err := apply.WriteAtomic(a.paths.SSHConfig, []byte(next), 0o644); err != nil {
		return a.fail(err)
	}
	return exitOK
}

// settleAccounts checks every alias and walks the human through uploading any
// key the server does not recognise yet.
func (a *app) settleAccounts(ctx context.Context, f *config.Profiles, opts uploadOpts) int {
	failed := false

	for i := range f.Profiles {
		p := &f.Profiles[i]

		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		res, err := verify.SSH(c, p.Alias)
		cancel()

		switch {
		case err != nil:
			fmt.Fprintf(a.errOut, "%s: %v\n", p.Name, err)
			failed = true
		case res.Authenticated:
			fmt.Fprintf(a.out, "%-12s authenticates as %s\n", p.Name, res.Username)
		default:
			if err := a.uploadKey(ctx, f, p, opts); err != nil {
				fmt.Fprintf(a.errOut, "%s: %v\n", p.Name, err)
				failed = true
			}
		}
	}

	if failed {
		fmt.Fprintln(a.errOut, "\nsome profiles are not working yet. run ghprofile check")
		return exitError
	}

	fmt.Fprintln(a.out, "\neverything is set up. to clone with a given identity:")
	for i := range f.Profiles {
		p := &f.Profiles[i]
		fmt.Fprintf(a.out, "\n  %s\n    git clone git@%s:OWNER/REPO.git\n", p.Name, p.Alias)
		if len(p.Dirs) > 0 {
			fmt.Fprintf(a.out, "    run that inside %s\n", p.Dirs[0])
		}
	}
	return exitOK
}

func (a *app) cmdAdopt(args []string) int {
	fset := a.flags("adopt")
	dry := fset.Bool("dry-run", false, "show what would be removed")
	if err := fset.Parse(args); err != nil {
		return exitMisuse
	}

	f, code := a.load()
	if f == nil {
		return code
	}

	data, err := os.ReadFile(a.paths.SSHConfig)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(a.out, "no ssh config to adopt from")
		return exitOK
	}
	if err != nil {
		return a.fail(err)
	}

	content := string(data)
	next := content
	var adopted []string
	for _, p := range f.Profiles {
		stripped := sshconfig.RemoveHost(next, p.Alias)
		if stripped != next {
			adopted = append(adopted, p.Alias)
			next = stripped
		}
	}

	if len(adopted) == 0 {
		fmt.Fprintln(a.out, "no hand-written stanzas match your profiles")
		return exitOK
	}

	for _, alias := range adopted {
		fmt.Fprintf(a.out, "adopt  Host %s\n", alias)
	}
	if *dry {
		fmt.Fprintf(a.out, "\n%d stanza(s) not removed (dry run)\n", len(adopted))
		return exitOK
	}

	if _, err := apply.NewBackups().Save(a.paths.SSHConfig); err != nil {
		return a.fail(err)
	}
	if err := apply.WriteAtomic(a.paths.SSHConfig, []byte(next), 0o644); err != nil {
		return a.fail(err)
	}
	fmt.Fprintf(a.out, "\nremoved %d stanza(s). run: ghprofile apply\n", len(adopted))
	return exitOK
}

func (a *app) cmdVerify(ctx context.Context, args []string) int {
	fset := a.flags("verify")
	timeout := fset.Duration("timeout", 20*time.Second, "how long to wait for each alias")
	positional, err := parse(fset, args)
	if err != nil {
		return exitMisuse
	}

	f, code := a.load()
	if f == nil {
		return code
	}

	only := ""
	if len(positional) > 0 {
		only = positional[0]
		if _, ok := f.ByName(only); !ok {
			return a.fail(fmt.Errorf("%q: %w", only, config.ErrUnknownProfile))
		}
	}

	seen := map[string]string{}
	failed := false

	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROFILE\tALIAS\tACCOUNT\tRESULT")

	for _, p := range f.Profiles {
		if only != "" && p.Name != only {
			continue
		}

		c, cancel := context.WithTimeout(ctx, *timeout)
		res, err := verify.SSH(c, p.Alias)
		cancel()

		switch {
		case err != nil:
			failed = true
			fmt.Fprintf(w, "%s\t%s\t-\terror: %v\n", p.Name, p.Alias, err)
		case !res.Authenticated:
			failed = true
			fmt.Fprintf(w, "%s\t%s\t-\tnot authenticated\n", p.Name, p.Alias)
		default:
			note := "ok"
			if other, dup := seen[res.Username]; dup {
				failed = true
				note = "same account as " + other
			}
			seen[res.Username] = p.Name
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, p.Alias, res.Username, note)
		}
	}

	if err := w.Flush(); err != nil {
		return a.fail(err)
	}
	if failed {
		return exitError
	}
	return exitOK
}
