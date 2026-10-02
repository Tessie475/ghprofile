package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/gitconfig"
	"github.com/Tessie475/ghprofile/internal/keys"
	"github.com/Tessie475/ghprofile/internal/plan"
	"github.com/Tessie475/ghprofile/internal/shell"
	"github.com/Tessie475/ghprofile/internal/sshconfig"
)

type finding struct {
	level string
	area  string
	text  string
}

func (a *app) cmdCheck(ctx context.Context, args []string) int {
	if err := a.flags("check").Parse(args); err != nil {
		return exitMisuse
	}

	f, code := a.load()
	if f == nil {
		return code
	}

	var found []finding

	for _, p := range f.Profiles {
		k, err := keys.Inspect(p.Key)
		if err != nil {
			found = append(found, finding{"warn", p.Name, err.Error()})
			continue
		}
		switch {
		case k.Orphaned():
			found = append(found, finding{"warn", p.Name,
				fmt.Sprintf("%s exists with no private key", k.PublicPath)})
		case !k.HasPrivate:
			found = append(found, finding{"warn", p.Name,
				fmt.Sprintf("no key at %s, run ghprofile apply", p.Key)})
		case !k.HasPublic:
			found = append(found, finding{"warn", p.Name,
				fmt.Sprintf("private key at %s has no matching .pub", p.Key)})
		case !k.PermissionsOK():
			found = append(found, finding{"warn", p.Name,
				fmt.Sprintf("%s is mode %v, OpenSSH wants %v", p.Key, k.Mode.Perm(), keys.PrivateMode)})
		}
	}

	found = append(found, a.orphanKeys()...)
	found = append(found, a.strayKeyMaterial()...)
	found = append(found, a.unmanagedHosts(f)...)
	found = append(found, a.conflictingGlobalIdentity(f)...)
	found = append(found, a.unclaimedHostname(f)...)
	found = append(found, a.staleRemotes(ctx, f)...)
	found = append(found, a.drift(f)...)

	if len(found) == 0 {
		fmt.Fprintln(a.out, "no problems found")
		return exitOK
	}

	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "LEVEL\tAREA\tFINDING")
	for _, fi := range found {
		fmt.Fprintf(w, "%s\t%s\t%s\n", fi.level, fi.area, fi.text)
	}
	if err := w.Flush(); err != nil {
		return a.fail(err)
	}

	fmt.Fprintf(a.out, "\n%d finding(s)\n", len(found))
	return exitOK
}

// orphanKeys reports public keys in ~/.ssh with no private half.
func (a *app) orphanKeys() []finding {
	entries, err := os.ReadDir(a.paths.SSHDir)
	if err != nil {
		return nil
	}

	var out []finding
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		pub := filepath.Join(a.paths.SSHDir, e.Name())
		priv := strings.TrimSuffix(pub, ".pub")
		if _, err := os.Stat(priv); err == nil {
			continue
		}
		out = append(out, finding{"warn", "ssh", fmt.Sprintf("%s has no private key", pub)})
	}
	return out
}

// strayKeyMaterial reports key files sitting loose in the home directory.
//
// It reads each file rather than trusting the extension, because a private key
// saved as something.pub is a mistake people really make, and calling that a
// harmless public key is worse than saying nothing.
func (a *app) strayKeyMaterial() []finding {
	entries, err := os.ReadDir(a.home)
	if err != nil {
		return nil
	}

	var out []finding
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.Contains(name, ".pub") && !strings.HasPrefix(name, "id_") && !strings.Contains(name, "_rsa") && !strings.Contains(name, "ed25519") {
			continue
		}

		path := filepath.Join(a.home, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		switch {
		case keys.LooksPrivate(data):
			level := "warn"
			detail := "private key material outside ~/.ssh"
			if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
				level = "warn"
				detail = fmt.Sprintf("private key material outside ~/.ssh, and mode %v lets others read it", info.Mode().Perm())
			}
			out = append(out, finding{level, "home", fmt.Sprintf("%s is %s", path, detail)})

		case keys.LooksPublic(data):
			out = append(out, finding{"info", "home",
				fmt.Sprintf("%s is a public key sitting outside ~/.ssh", path)})
		}
	}
	return out
}

func (a *app) unmanagedHosts(f *config.Profiles) []finding {
	data, err := os.ReadFile(a.paths.SSHConfig)
	if err != nil {
		return nil
	}

	var out []finding
	for _, h := range sshconfig.Shadowed(string(data), f) {
		out = append(out, finding{"warn", "ssh",
			fmt.Sprintf("Host %s is also declared by hand and would win, run ghprofile adopt", h)})
	}
	for _, h := range sshconfig.Unmanaged(string(data), f) {
		out = append(out, finding{"info", "ssh",
			fmt.Sprintf("Host %s is not managed by any profile", h)})
	}
	return out
}

func (a *app) drift(f *config.Profiles) []finding {
	st, err := plan.ReadState(f, a.paths)
	if err != nil {
		return []finding{{"warn", "state", err.Error()}}
	}
	actions, err := plan.Compute(f, st, a.paths, a.darwin)
	if err != nil {
		return []finding{{"warn", "plan", err.Error()}}
	}
	if len(actions) == 0 {
		return nil
	}
	return []finding{{"warn", "drift",
		fmt.Sprintf("%d change(s) pending, run ghprofile plan", len(actions))}}
}

// staleRemotes reports repositories whose origin does not use the host alias
// of the profile that governs their directory.
func (a *app) staleRemotes(ctx context.Context, f *config.Profiles) []finding {
	var out []finding

	for _, p := range f.Profiles {
		for _, dir := range p.Dirs {
			for _, repo := range findRepos(dir, maxScanDepth) {
				res, err := shell.Run(ctx, "git", "-C", repo, "remote", "get-url", "origin")
				if err != nil || res.Code != 0 {
					continue
				}

				current := strings.TrimSpace(res.Stdout)
				owner, ok := f.ForDir(repo)
				if !ok {
					continue
				}
				if _, changed := rewriteRemote(current, owner, f); changed {
					out = append(out, finding{"warn", owner.Name,
						fmt.Sprintf("%s uses %s, run ghprofile fix-remote", repo, current)})
				}
			}
		}
	}
	return out
}

// conflictingGlobalIdentity reports a hand-written [user] section that
// overrides the default profile, because it sits below the managed block.
func (a *app) conflictingGlobalIdentity(f *config.Profiles) []finding {
	d, ok := f.DefaultProfile()
	if !ok {
		return nil
	}

	data, err := os.ReadFile(a.paths.GitConfig)
	if err != nil {
		return nil
	}

	email, found := gitconfig.HandWrittenEmail(string(data))
	if !found || email == d.User.Email {
		return nil
	}
	return []finding{{"warn", "gitconfig",
		fmt.Sprintf("a [user] section sets %s and overrides the %q profile's %s, remove it to let ghprofile own your global identity",
			email, d.Name, d.User.Email)}}
}

// unclaimedHostname notes that plain URLs are resolved by the ssh agent rather
// than by configuration. It reports and does not act, because claiming the
// hostname restricts every plain connection to that host.
func (a *app) unclaimedHostname(f *config.Profiles) []finding {
	d, ok := f.DefaultProfile()
	if !ok || d.ClaimHost {
		return nil
	}
	return []finding{{"info", "ssh",
		fmt.Sprintf("a plain git@%s URL uses whichever key your agent offers, not %q. to pin it: ghprofile default %s -claim-host",
			d.Host, d.Name, d.Name)}}
}
