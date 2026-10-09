package gitconfig

import (
	"strings"
	"testing"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/paths"
)

func testProfiles() *config.Profiles {
	return &config.Profiles{
		Version: config.Version,
		Profiles: []config.Profile{
			{Name: "personal", Host: "github.com", Alias: "github-personal", Key: "/k/p", Default: true,
				User: config.User{Name: "A", Email: "a@example.com"}},
			{Name: "work", Host: "github.com", Alias: "github-work", Key: "/k/w", Dirs: []string{"/home/u/work/"},
				User: config.User{Name: "B", Email: "b@example.org"}},
		},
	}
}

func TestIdentity(t *testing.T) {
	got := Identity(testProfiles().Profiles[1])

	if !strings.Contains(got, "[user]") {
		t.Errorf("missing [user] section:\n%s", got)
	}
	if !strings.Contains(got, "email = b@example.org") {
		t.Errorf("missing email:\n%s", got)
	}
}

func TestRenderDefault(t *testing.T) {
	pa := paths.Default("/home/u")
	got := RenderDefault(testProfiles(), pa)

	if !strings.Contains(got, "[include]") {
		t.Errorf("default profile has no unconditional include:\n%s", got)
	}
	if !strings.Contains(got, pa.IdentityFile("personal")) {
		t.Errorf("include does not point at the default identity:\n%s", got)
	}
	if strings.Contains(got, "includeIf") {
		t.Errorf("the default block must hold no conditional includes:\n%s", got)
	}
}

func TestRenderIncludes(t *testing.T) {
	pa := paths.Default("/home/u")
	got := RenderIncludes(testProfiles(), pa)

	if !strings.Contains(got, `[includeIf "gitdir:/home/u/work/"]`) {
		t.Errorf("missing conditional include:\n%s", got)
	}
	if !strings.Contains(got, pa.IdentityFile("work")) {
		t.Errorf("conditional include does not point at the work identity:\n%s", got)
	}
	if strings.Contains(got, "[include]\n") {
		t.Errorf("the includes block must hold no unconditional include:\n%s", got)
	}
}

func TestApply_PreservesOtherSections(t *testing.T) {
	const existing = "[pull]\n\trebase = true\n[filter \"lfs\"]\n\trequired = true\n"

	got, err := Apply(existing, testProfiles(), paths.Default("/home/u"))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for _, want := range []string{"[pull]", "rebase = true", "[filter \"lfs\"]", "required = true"} {
		if !strings.Contains(got, want) {
			t.Errorf("existing config lost %q:\n%s", want, got)
		}
	}
}

func TestApply_IsIdempotent(t *testing.T) {
	pa := paths.Default("/home/u")

	once, err := Apply("[pull]\n\trebase = true\n", testProfiles(), pa)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	twice, err := Apply(once, testProfiles(), pa)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if once != twice {
		t.Error("Apply is not idempotent")
	}
}

func TestHandWrittenEmail(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantOK  bool
	}{
		{
			name:    "plain user section",
			content: "[user]\n\tname = A\n\temail = a@example.com\n",
			want:    "a@example.com",
			wantOK:  true,
		},
		{
			name:    "no user section",
			content: "[pull]\n\trebase = true\n",
			wantOK:  false,
		},
		{
			name:    "a user section inside a managed block is ours, not theirs",
			content: "# BEGIN ghprofile:x\n[user]\n\temail = ours@example.com\n# END ghprofile:x\n",
			wantOK:  false,
		},
		{
			name:    "email in a later section is not the user's",
			content: "[user]\n\tname = A\n[credential]\n\temail = nope@example.com\n",
			wantOK:  false,
		},
		{
			name:    "blank email is not a value",
			content: "[user]\n\temail =\n",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := HandWrittenEmail(tt.content)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("email = %q, want %q", got, tt.want)
			}
		})
	}
}

// A Windows path written raw into a git config makes \U and \G invalid
// escapes, and git rejects the entire file with "bad config line 3". Observed
// on a real machine: every git command failed until the block was removed.
func TestToGitPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		windows bool
		want    string
	}{
		{
			name:    "windows path is converted",
			path:    `C:\Users\GOKU\.config\ghprofile\gitconfig-personal`,
			windows: true,
			want:    "C:/Users/GOKU/.config/ghprofile/gitconfig-personal",
		},
		{
			name:    "windows gitdir pattern, mixed separators",
			path:    `C:\Users\GOKU\test-work/`,
			windows: true,
			want:    "C:/Users/GOKU/test-work/",
		},
		{
			name:    "unix path is untouched",
			path:    "/Users/me/.config/ghprofile/gitconfig-personal",
			windows: false,
			want:    "/Users/me/.config/ghprofile/gitconfig-personal",
		},
		{
			// A backslash is a legal character in a Unix filename, so
			// converting unconditionally would corrupt it.
			name:    "a unix path containing a backslash is untouched",
			path:    `/Users/me/odd\name/gitconfig`,
			windows: false,
			want:    `/Users/me/odd\name/gitconfig`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toGitPath(tt.path, tt.windows); got != tt.want {
				t.Errorf("toGitPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every path in both blocks must be git-safe when rendered for Windows. This
// forces the Windows branch rather than waiting to run on Windows, because the
// version that waited never failed anywhere and let a raw path through.
func TestRender_WindowsPathsCarryNoBackslashes(t *testing.T) {
	saved := isWindows
	isWindows = true
	t.Cleanup(func() { isWindows = saved })

	f := &config.Profiles{Version: config.Version, Profiles: []config.Profile{
		{Name: "personal", Host: "github.com", Alias: "github-personal", Key: `C:\k\p`, Default: true,
			User: config.User{Name: "A", Email: "a@example.com"}},
		{Name: "work", Host: "github.com", Alias: "github-work", Key: `C:\k\w`,
			Dirs: []string{`C:\Users\GOKU\test-work/`},
			User: config.User{Name: "B", Email: "b@example.org"}},
	}}
	pa := paths.Default(`C:\Users\GOKU`)

	for name, body := range map[string]string{
		"default block":  RenderDefault(f, pa),
		"includes block": RenderIncludes(f, pa),
	} {
		if strings.Contains(body, `\`) {
			t.Errorf("%s carries a backslash, which git rejects as a bad escape:\n%s", name, body)
		}
		if !strings.Contains(body, "C:/Users/GOKU") {
			t.Errorf("%s does not contain the converted path:\n%s", name, body)
		}
	}
}
