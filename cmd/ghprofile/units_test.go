package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/paths"
)

// A wrong settings URL is the first thing a new user sees during the upload
// handoff, so it is worth pinning per provider.
func TestKeyURL(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{"github", "github.com", "https://github.com/settings/keys"},
		{"github, odd casing", "GitHub.com", "https://github.com/settings/keys"},
		{"gitlab.com", "gitlab.com", "https://gitlab.com/-/user_settings/ssh_keys"},
		{"self-hosted gitlab", "gitlab.example.com", "https://gitlab.example.com/-/user_settings/ssh_keys"},
		{"self-hosted gitlab, capitalised", "GitLab.Example.COM", "https://GitLab.Example.COM/-/user_settings/ssh_keys"},
		{"anything else falls back to the host", "git.elunic.software", "https://git.elunic.software"},
		{"bitbucket", "bitbucket.org", "https://bitbucket.org"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keyURL(tt.host); got != tt.want {
				t.Errorf("keyURL(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestDefaultAlias(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		profile string
		want    string
	}{
		{"github", "github.com", "work", "github-work"},
		{"github enterprise", "github.company.com", "work", "github-work"},
		{"gitlab", "gitlab.com", "clientx", "gitlab-clientx"},
		{"bitbucket", "bitbucket.org", "oss", "bitbucket-oss"},
		// git, www and the TLDs are skipped, so the alias names the
		// organisation rather than a meaningless label.
		{"self-hosted behind a git subdomain", "git.elunic.software", "elunic", "elunic-elunic"},
		{"self-hosted behind www", "www.example.org", "acme", "example-acme"},
		{"bare host", "example.com", "acme", "example-acme"},
		{"casing is ignored", "GITHUB.COM", "work", "github-work"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultAlias(tt.host, tt.profile); got != tt.want {
				t.Errorf("defaultAlias(%q, %q) = %q, want %q", tt.host, tt.profile, got, tt.want)
			}
		})
	}
}

// Two profiles sharing a private key cannot authenticate as different
// accounts, which is the one mistake that silently defeats the whole tool.
func TestKeyTaken(t *testing.T) {
	f := &config.Profiles{Profiles: []config.Profile{
		{Name: "personal", Key: "/home/u/.ssh/personal"},
	}}

	if !keyTaken(f, "/home/u/.ssh/personal", "/home/u") {
		t.Error("an absolute path already in use was reported as free")
	}
	if !keyTaken(f, "~/.ssh/personal", "/home/u") {
		t.Error("the same key in ~ form was reported as free")
	}
	if keyTaken(f, "~/.ssh/other", "/home/u") {
		t.Error("an unused key was reported as taken")
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"single line", "Permission denied\n", "Permission denied"},
		{"leading blank lines are skipped", "\n\n  real content\n", "real content"},
		{"empty", "", "no output"},
		{"whitespace only", "   \n\t\n", "no output"},
		{"multiline takes the first", "first\nsecond\n", "first"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLine(tt.input); got != tt.want {
				t.Errorf("firstLine(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFindRepos(t *testing.T) {
	root := t.TempDir()

	for _, dir := range []string{
		"one/.git",
		"nested/two/.git",
		"nested/two/vendored/.git", // inside a repo, must not be returned
		"node_modules/pkg/.git",    // skipped by name
		"vendor/dep/.git",          // skipped by name
		"notarepo/src",
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	got := findRepos(root, maxScanDepth)

	var names []string
	for _, p := range got {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		names = append(names, rel)
	}

	want := map[string]bool{"one": true, "nested/two": true}
	if len(names) != len(want) {
		t.Fatalf("findRepos() = %v, want exactly %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("findRepos() returned %q, which should have been skipped", n)
		}
	}
}

func TestFindRepos_RespectsTheDepthLimit(t *testing.T) {
	root := t.TempDir()

	deep := filepath.Join(root, "a/b/c/d/e/f/g/h/repo/.git")
	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if got := findRepos(root, 2); len(got) != 0 {
		t.Errorf("findRepos(depth 2) = %v, want none that deep", got)
	}
	if got := findRepos(root, 20); len(got) != 1 {
		t.Errorf("findRepos(depth 20) = %v, want the one repo", got)
	}
}

func TestFindRepos_MissingRootIsNotFatal(t *testing.T) {
	if got := findRepos(filepath.Join(t.TempDir(), "absent"), maxScanDepth); len(got) != 0 {
		t.Errorf("findRepos(absent) = %v, want none", got)
	}
}

// Carrying directives across is what stops adopt silently dropping a
// hand-written ServerAliveInterval or ProxyJump.
func TestInheritedOptions(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}

	const content = `Host github-work
  HostName github.com
  IdentityFile ~/.ssh/id_work
  IdentitiesOnly yes
  ServerAliveInterval 60
  ProxyJump bastion

Host github-other
  HostName github.com
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(content), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	a := newTestApp(t, home)

	got := a.inheritedOptions("github-work")
	if got["ServerAliveInterval"] != "60" {
		t.Errorf("ServerAliveInterval = %q, want 60", got["ServerAliveInterval"])
	}
	if got["ProxyJump"] != "bastion" {
		t.Errorf("ProxyJump = %q, want bastion", got["ProxyJump"])
	}
	// Directives Render always writes must never be inherited, or the stanza
	// would carry them twice.
	if _, found := got["IdentitiesOnly"]; found {
		t.Error("IdentitiesOnly was inherited, which would duplicate it")
	}
	if _, found := got["IdentityFile"]; found {
		t.Error("IdentityFile was inherited, which would duplicate it")
	}

	if got := a.inheritedOptions("github-other"); len(got) != 0 {
		t.Errorf("inheritedOptions(no extras) = %v, want none", got)
	}
	if got := a.inheritedOptions("github-absent"); len(got) != 0 {
		t.Errorf("inheritedOptions(absent alias) = %v, want none", got)
	}
}

// The regression test for the bug where every profile on a host got the first
// stanza's key. Matching must be on the alias, never the hostname.
func TestSuggestKey(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}

	const content = `Host github-personal
  HostName github.com
  IdentityFile ~/.ssh/personal_github

Host github-work
  HostName github.com
  IdentityFile ~/.ssh/id_work
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(content), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	a := newTestApp(t, home)
	empty := &config.Profiles{}

	if got := a.suggestKey(empty, "github-personal", "personal"); got != "~/.ssh/personal_github" {
		t.Errorf("suggestKey(github-personal) = %q, want the stanza's key", got)
	}
	if got := a.suggestKey(empty, "github-work", "work"); got != "~/.ssh/id_work" {
		t.Errorf("suggestKey(github-work) = %q, want the stanza's key", got)
	}

	// An alias with no stanza gets its own path, not another profile's.
	if got := a.suggestKey(empty, "github-new", "new"); got != "~/.ssh/id_ed25519_new" {
		t.Errorf("suggestKey(unknown alias) = %q, want a per-profile default", got)
	}

	// And a key another profile already holds is never handed out twice.
	taken := &config.Profiles{Profiles: []config.Profile{
		{Name: "personal", Key: filepath.Join(home, ".ssh", "personal_github")},
	}}
	if got := a.suggestKey(taken, "github-personal", "second"); got != "~/.ssh/id_ed25519_second" {
		t.Errorf("suggestKey(taken key) = %q, want a fresh path", got)
	}
}

func TestSuggestKey_NoSSHConfig(t *testing.T) {
	a := newTestApp(t, t.TempDir())

	if got := a.suggestKey(&config.Profiles{}, "github-work", "work"); got != "~/.ssh/id_ed25519_work" {
		t.Errorf("suggestKey() = %q, want the default when there is no config", got)
	}
}

// newTestApp builds an app rooted at a throwaway home.
func newTestApp(t *testing.T, home string) *app {
	t.Helper()
	return &app{
		in:     strings.NewReader("\n"),
		out:    &strings.Builder{},
		errOut: &strings.Builder{},
		home:   home,
		paths:  paths.Default(home),
		darwin: true,
	}
}
