// Package plan decides what needs to change, without changing anything.
package plan

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/gitconfig"
	"github.com/Tessie475/ghprofile/internal/keys"
	"github.com/Tessie475/ghprofile/internal/paths"
	"github.com/Tessie475/ghprofile/internal/sshconfig"
)

// Kind names the operations apply knows how to carry out.
type Kind string

// The operations apply knows how to carry out.
const (
	MakeDir   Kind = "create directory"
	MakeKey   Kind = "generate key"
	WriteFile Kind = "write"
	Chmod     Kind = "fix permissions"
)

// Action is one unit of work. Nothing here performs it.
type Action struct {
	Kind    Kind
	Path    string
	Mode    fs.FileMode
	Content string
	Before  string
	Comment string
	Reason  string
}

// State is a snapshot of the machine as it is now.
type State struct {
	SSHConfig  string
	GitConfig  string
	Identities map[string]string
	Keys       map[string]keys.Key
	Dirs       map[string]bool
}

// Compute is pure: it performs no IO and consults no clock or environment.
func Compute(f *config.Profiles, st State, pa paths.Paths, darwin bool) ([]Action, error) {
	var actions []Action

	for _, dir := range []string{pa.SSHDir, pa.ConfigDir} {
		if !st.Dirs[dir] {
			actions = append(actions, Action{
				Kind: MakeDir, Path: dir, Mode: keys.DirMode,
				Reason: "directory does not exist",
			})
		}
	}

	for _, p := range f.Profiles {
		k := st.Keys[p.Key]
		switch {
		case !k.HasPrivate:
			actions = append(actions, Action{
				Kind: MakeKey, Path: p.Key, Mode: keys.PrivateMode,
				Comment: p.User.Email,
				Reason:  fmt.Sprintf("profile %q has no private key", p.Name),
			})
		case !k.PermissionsOK():
			actions = append(actions, Action{
				Kind: Chmod, Path: p.Key, Mode: keys.PrivateMode,
				Reason: fmt.Sprintf("mode is %v, OpenSSH requires %v", k.Mode.Perm(), keys.PrivateMode),
			})
		}
	}

	for _, p := range f.Profiles {
		path := pa.IdentityFile(p.Name)
		want := gitconfig.Identity(p)
		if got := st.Identities[p.Name]; got != want {
			actions = append(actions, Action{
				Kind: WriteFile, Path: path, Mode: 0o644,
				Content: want, Before: got,
				Reason: fmt.Sprintf("git identity for %q", p.Name),
			})
		}
	}

	nextSSH, err := sshconfig.Apply(st.SSHConfig, f, darwin)
	if err != nil {
		return nil, fmt.Errorf("ssh config: %w", err)
	}
	if nextSSH != st.SSHConfig {
		actions = append(actions, Action{
			Kind: WriteFile, Path: pa.SSHConfig, Mode: 0o644,
			Content: nextSSH, Before: st.SSHConfig,
			Reason: "host aliases out of date",
		})
	}

	nextGit, err := gitconfig.Apply(st.GitConfig, f, pa)
	if err != nil {
		return nil, fmt.Errorf("git config: %w", err)
	}
	if nextGit != st.GitConfig {
		actions = append(actions, Action{
			Kind: WriteFile, Path: pa.GitConfig, Mode: 0o644,
			Content: nextGit, Before: st.GitConfig,
			Reason: "identity includes out of date",
		})
	}

	return actions, nil
}

// String renders one action as a single line.
func (a Action) String() string {
	return fmt.Sprintf("%-18s %s", string(a.Kind), a.Path)
}

// Diff renders the change to a file as added and removed lines. It trims the
// common prefix and suffix so that only the interesting middle is shown.
func (a Action) Diff() string {
	if a.Kind != WriteFile {
		return ""
	}

	before := lines(a.Before)
	after := lines(a.Content)

	head := 0
	for head < len(before) && head < len(after) && before[head] == after[head] {
		head++
	}
	tail := 0
	for tail < len(before)-head && tail < len(after)-head &&
		before[len(before)-1-tail] == after[len(after)-1-tail] {
		tail++
	}

	var b strings.Builder
	for _, ln := range before[head : len(before)-tail] {
		fmt.Fprintf(&b, "    - %s\n", ln)
	}
	for _, ln := range after[head : len(after)-tail] {
		fmt.Fprintf(&b, "    + %s\n", ln)
	}
	return b.String()
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
