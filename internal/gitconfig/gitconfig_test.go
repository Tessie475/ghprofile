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
