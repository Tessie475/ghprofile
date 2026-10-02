package gitconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/paths"
)

// This is the regression test for the bug where an unconditional default
// include, appended at the end of the file, silently overrode a conditional
// include the user had written by hand higher up. Git applies the last
// matching setting, so placement is the whole of the behaviour, and the only
// honest way to check it is to ask git.
func TestApply_DoesNotHijackForeignConditionalIncludes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	// macOS hands out /var/folders/... while git canonicalises repository paths
	// to /private/var/folders/..., and gitdir: matching is done on the resolved
	// path. Without this the patterns silently never match.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	pa := paths.Default(home)

	foreignDir := filepath.Join(home, "elsewhere") + "/"
	mustWrite(t, filepath.Join(home, ".gitconfig-foreign"), "[user]\n\tname = Foreign\n\temail = foreign@example.com\n")

	// A hand-written config: a global identity, then a conditional include the
	// user set up themselves for a directory ghprofile knows nothing about.
	existing := "[user]\n\tname = Global\n\temail = global@example.com\n" +
		"[includeIf \"gitdir:" + foreignDir + "\"]\n\tpath = " + filepath.Join(home, ".gitconfig-foreign") + "\n"

	managedDir := filepath.Join(home, "managed") + "/"
	f := &config.Profiles{Version: config.Version, Profiles: []config.Profile{
		{Name: "personal", Host: "github.com", Alias: "github-personal", Key: "/k/p", Default: true,
			User: config.User{Name: "Personal", Email: "personal@example.com"}},
		{Name: "second", Host: "github.com", Alias: "github-second", Key: "/k/s", Dirs: []string{managedDir},
			User: config.User{Name: "Second", Email: "second@example.com"}},
	}}

	out, err := Apply(existing, f, pa)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if err := os.MkdirAll(pa.ConfigDir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, p := range f.Profiles {
		mustWrite(t, pa.IdentityFile(p.Name), Identity(p))
	}
	mustWrite(t, pa.GitConfig, out)

	tests := []struct {
		name string
		dir  string
		want string
	}{
		{"a directory ghprofile does not manage keeps the user's own identity", foreignDir, "foreign@example.com"},
		{"a directory ghprofile manages gets its profile identity", managedDir, "second@example.com"},
		// The user's own [user] section sits below the default-identity block,
		// so it wins. check reports the conflict rather than the tool fighting
		// it, because moving the block lower would break the includes above.
		{"anywhere else keeps the user's own global identity", filepath.Join(home, "other") + "/", "global@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := filepath.Join(tt.dir, "repo")
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatalf("setup: %v", err)
			}
			run(t, home, "git", "init", "-q", repo)

			got := strings.TrimSpace(run(t, home, "git", "-C", repo, "config", "user.email"))
			if got != tt.want {
				t.Errorf("git resolves %q, want %q\n\n--- .gitconfig ---\n%s", got, tt.want, out)
			}
		})
	}
}

func TestApply_DefaultBlockComesBeforeIncludesBlock(t *testing.T) {
	pa := paths.Default("/home/u")
	f := &config.Profiles{Version: config.Version, Profiles: []config.Profile{
		{Name: "personal", Host: "github.com", Alias: "github-personal", Key: "/k/p", Default: true,
			User: config.User{Name: "A", Email: "a@example.com"}},
		{Name: "work", Host: "github.com", Alias: "github-work", Key: "/k/w", Dirs: []string{"/home/u/work/"},
			User: config.User{Name: "B", Email: "b@example.org"}},
	}}

	out, err := Apply("[pull]\n\trebase = true\n", f, pa)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	def := strings.Index(out, "ghprofile:"+DefaultBlockName)
	inc := strings.Index(out, "ghprofile:"+BlockName)
	user := strings.Index(out, "[pull]")

	if def < 0 || inc < 0 {
		t.Fatalf("a managed block is missing:\n%s", out)
	}
	if def > user {
		t.Error("the default block must sit above the user's own config")
	}
	if inc < user {
		t.Error("the includes block must sit below the user's own config")
	}
}

func TestApply_NoDefaultProfileWritesNoDefaultBlock(t *testing.T) {
	pa := paths.Default("/home/u")
	f := &config.Profiles{Version: config.Version, Profiles: []config.Profile{
		{Name: "work", Host: "github.com", Alias: "github-work", Key: "/k/w", Dirs: []string{"/home/u/work/"},
			User: config.User{Name: "B", Email: "b@example.org"}},
	}}

	out, err := Apply("", f, pa)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if strings.Contains(out, DefaultBlockName) {
		t.Errorf("wrote a default block with no default profile:\n%s", out)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func run(t *testing.T, home string, name string, args ...string) string {
	t.Helper()
	// CommandContext, so a hung git cannot outlive the test.
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL="+filepath.Join(home, ".gitconfig"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}
