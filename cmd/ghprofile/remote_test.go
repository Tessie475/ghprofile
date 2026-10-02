package main

import (
	"testing"

	"github.com/Tessie475/ghprofile/internal/config"
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

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name     string
		remote   string
		wantHost string
		wantPath string
		wantOK   bool
	}{
		{"scp form", "git@github.com:ORG/repo.git", "github.com", "ORG/repo.git", true},
		{"scp form with alias", "git@github-work:ORG/repo.git", "github-work", "ORG/repo.git", true},
		{"https", "https://github.com/ORG/repo.git", "github.com", "ORG/repo.git", true},
		{"https with no suffix", "https://gitlab.example.com/team/repo", "gitlab.example.com", "team/repo", true},
		{"ssh url", "ssh://git@github.com/ORG/repo.git", "github.com", "ORG/repo.git", true},
		{"local path", "/srv/git/repo.git", "", "", false},
		{"nonsense", "not a url", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, path, ok := parseRemote(tt.remote)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
		})
	}
}

func TestRewriteRemote(t *testing.T) {
	f := testProfiles()
	work := &f.Profiles[1]

	tests := []struct {
		name        string
		remote      string
		want        string
		wantChanged bool
	}{
		{
			name:        "https becomes the alias",
			remote:      "https://github.com/ORG/repo.git",
			want:        "git@github-work:ORG/repo.git",
			wantChanged: true,
		},
		{
			name:        "plain ssh becomes the alias",
			remote:      "git@github.com:ORG/repo.git",
			want:        "git@github-work:ORG/repo.git",
			wantChanged: true,
		},
		{
			name:        "the wrong alias is corrected",
			remote:      "git@github-personal:ORG/repo.git",
			want:        "git@github-work:ORG/repo.git",
			wantChanged: true,
		},
		{
			name:        "already correct",
			remote:      "git@github-work:ORG/repo.git",
			want:        "git@github-work:ORG/repo.git",
			wantChanged: false,
		},
		{
			name:        "a host we do not own is left alone",
			remote:      "git@bitbucket.org:ORG/repo.git",
			want:        "git@bitbucket.org:ORG/repo.git",
			wantChanged: false,
		},
		{
			name:        "unparseable is left alone",
			remote:      "/srv/git/repo.git",
			want:        "/srv/git/repo.git",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := rewriteRemote(tt.remote, work, f)
			if changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if got != tt.want {
				t.Errorf("remote = %q, want %q", got, tt.want)
			}
		})
	}
}
