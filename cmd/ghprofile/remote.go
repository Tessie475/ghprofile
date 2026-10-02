package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/shell"
)

const maxScanDepth = 5

func (a *app) cmdFixRemote(ctx context.Context, args []string) int {
	fset := a.flags("fix-remote")
	write := fset.Bool("write", false, "actually rewrite the remotes")
	all := fset.Bool("all", false, "scan every directory named by a profile")
	positional, err := parse(fset, args)
	if err != nil {
		return exitMisuse
	}

	f, code := a.load()
	if f == nil {
		return code
	}

	roots, code := a.scanRoots(f, positional, *all)
	if roots == nil {
		return code
	}

	var pending int
	for _, root := range roots {
		for _, repo := range findRepos(root, maxScanDepth) {
			changed, err := a.fixRepo(ctx, f, repo, *write)
			if err != nil {
				fmt.Fprintf(a.errOut, "ghprofile: %s: %v\n", repo, err)
				continue
			}
			if changed {
				pending++
			}
		}
	}

	switch {
	case pending == 0:
		fmt.Fprintln(a.out, "every remote already uses the right host alias")
	case *write:
		fmt.Fprintf(a.out, "\nrewrote %d remote(s)\n", pending)
	default:
		fmt.Fprintf(a.out, "\n%d remote(s) would change. re-run with -write\n", pending)
	}
	return exitOK
}

func (a *app) scanRoots(f *config.Profiles, positional []string, all bool) ([]string, int) {
	if all {
		var roots []string
		for _, p := range f.Profiles {
			roots = append(roots, p.Dirs...)
		}
		if len(roots) == 0 {
			fmt.Fprintln(a.errOut, "ghprofile: no profile declares any directories")
			return nil, exitError
		}
		return roots, exitOK
	}

	target := "."
	if len(positional) > 0 {
		target = positional[0]
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, a.fail(err)
	}
	return []string{abs}, exitOK
}

func (a *app) fixRepo(ctx context.Context, f *config.Profiles, repo string, write bool) (bool, error) {
	res, err := shell.Run(ctx, "git", "-C", repo, "remote", "get-url", "origin")
	if err != nil {
		return false, err
	}
	if res.Code != 0 {
		return false, nil
	}

	current := strings.TrimSpace(res.Stdout)
	p, ok := f.ForDir(repo)
	if !ok {
		return false, nil
	}

	want, changed := rewriteRemote(current, p, f)
	if !changed {
		return false, nil
	}

	fmt.Fprintf(a.out, "%s\n    - %s\n    + %s\n", repo, current, want)
	if !write {
		return true, nil
	}

	res, err = shell.Run(ctx, "git", "-C", repo, "remote", "set-url", "origin", want)
	if err != nil {
		return false, err
	}
	if res.Code != 0 {
		return false, fmt.Errorf("git remote set-url: %s", strings.TrimSpace(res.Stderr))
	}
	return true, nil
}

// rewriteRemote returns the SSH-alias form of a remote when the profile owns
// the host it points at.
func rewriteRemote(remote string, p *config.Profile, f *config.Profiles) (string, bool) {
	host, path, ok := parseRemote(remote)
	if !ok {
		return remote, false
	}

	owned := strings.EqualFold(host, p.Host) || strings.EqualFold(host, p.Alias)
	for _, other := range f.Profiles {
		if strings.EqualFold(host, other.Alias) {
			owned = true
		}
	}
	if !owned {
		return remote, false
	}

	want := fmt.Sprintf("git@%s:%s", p.Alias, path)
	return want, want != remote
}

func parseRemote(remote string) (host, path string, ok bool) {
	switch {
	case strings.HasPrefix(remote, "ssh://"):
		rest := strings.TrimPrefix(strings.TrimPrefix(remote, "ssh://"), "git@")
		i := strings.Index(rest, "/")
		if i <= 0 {
			return "", "", false
		}
		return rest[:i], strings.TrimPrefix(rest[i:], "/"), true

	case strings.HasPrefix(remote, "https://"), strings.HasPrefix(remote, "http://"):
		rest := remote[strings.Index(remote, "://")+3:]
		i := strings.Index(rest, "/")
		if i <= 0 {
			return "", "", false
		}
		return rest[:i], strings.TrimPrefix(rest[i:], "/"), true

	case strings.Contains(remote, "@") && strings.Contains(remote, ":"):
		at := strings.Index(remote, "@")
		colon := strings.Index(remote[at:], ":") + at
		if colon <= at+1 {
			return "", "", false
		}
		return remote[at+1 : colon], remote[colon+1:], true
	}
	return "", "", false
}

// findRepos returns every git working tree under root, without descending into
// one it has already found.
func findRepos(root string, maxDepth int) []string {
	var out []string
	rootDepth := strings.Count(filepath.Clean(root), string(os.PathSeparator))

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // an unreadable directory is skipped, not fatal
		}
		switch d.Name() {
		case "node_modules", "vendor", ".terraform":
			return filepath.SkipDir
		}
		if strings.Count(path, string(os.PathSeparator))-rootDepth > maxDepth {
			return filepath.SkipDir
		}
		if _, statErr := os.Stat(filepath.Join(path, ".git")); statErr == nil {
			out = append(out, path)
			return filepath.SkipDir
		}
		return nil
	})

	return out
}
