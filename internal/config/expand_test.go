package config

import (
	"errors"
	"strings"
	"testing"
)

func TestProfiles_ExpandPaths(t *testing.T) {
	const home = "/home/u"

	tests := []struct {
		name     string
		key      string
		dirs     []string
		wantKey  string
		wantDirs []string
		wantErr  error
	}{
		{
			name:     "tilde slash",
			key:      "~/.ssh/id_work",
			dirs:     []string{"~/work/"},
			wantKey:  "/home/u/.ssh/id_work",
			wantDirs: []string{"/home/u/work/"},
		},
		{
			name:     "bare tilde",
			key:      "~",
			dirs:     []string{"~"},
			wantKey:  "/home/u",
			wantDirs: []string{"/home/u/"},
		},
		{
			name:     "absolute is untouched",
			key:      "/opt/keys/id",
			dirs:     []string{"/srv/code/"},
			wantKey:  "/opt/keys/id",
			wantDirs: []string{"/srv/code/"},
		},
		{
			name:     "trailing slash added",
			key:      "~/.ssh/id",
			dirs:     []string{"~/work", "/srv/code"},
			wantKey:  "/home/u/.ssh/id",
			wantDirs: []string{"/home/u/work/", "/srv/code/"},
		},
		{
			name:     "redundant separators cleaned",
			key:      "~/.ssh//id",
			dirs:     []string{"~/work/./sub/"},
			wantKey:  "/home/u/.ssh/id",
			wantDirs: []string{"/home/u/work/sub/"},
		},
		{name: "another user's home", key: "~someone/.ssh/id", wantErr: ErrInvalidPath},
		{name: "relative path", key: "keys/id", wantErr: ErrInvalidPath},
		{name: "dot relative path", key: "./keys/id", wantErr: ErrInvalidPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Profiles{Version: Version, Profiles: []Profile{{
				Name: "work", Host: "github.com", Alias: "github-work",
				Key: tt.key, Dirs: tt.dirs,
				User: User{Name: "A", Email: "a@example.com"},
			}}}

			err := f.ExpandPaths(home)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ExpandPaths() error = %v, want it to wrap %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExpandPaths() error = %v", err)
			}

			if got := f.Profiles[0].Key; got != tt.wantKey {
				t.Errorf("Key = %q, want %q", got, tt.wantKey)
			}
			got := f.Profiles[0].Dirs
			if len(got) != len(tt.wantDirs) {
				t.Fatalf("Dirs = %v, want %v", got, tt.wantDirs)
			}
			for i := range got {
				if got[i] != tt.wantDirs[i] {
					t.Errorf("Dirs[%d] = %q, want %q", i, got[i], tt.wantDirs[i])
				}
			}
		})
	}
}

// Ranging over values instead of indices would make this pass silently wrong.
func TestProfiles_ExpandPathsTouchesEveryProfile(t *testing.T) {
	f := goodFile()
	for i := range f.Profiles {
		f.Profiles[i].Key = "~/.ssh/key"
		f.Profiles[i].Dirs = []string{"~/code"}
	}

	if err := f.ExpandPaths("/home/u"); err != nil {
		t.Fatalf("ExpandPaths() error = %v", err)
	}
	for i, p := range f.Profiles {
		if strings.HasPrefix(p.Key, "~") {
			t.Errorf("Profiles[%d].Key = %q, still unexpanded", i, p.Key)
		}
		if strings.HasPrefix(p.Dirs[0], "~") {
			t.Errorf("Profiles[%d].Dirs[0] = %q, still unexpanded", i, p.Dirs[0])
		}
	}
}
