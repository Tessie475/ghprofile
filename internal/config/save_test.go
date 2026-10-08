package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarshal_ContractsPaths(t *testing.T) {
	f := &Profiles{Version: Version, Profiles: []Profile{{
		Name: "work", Host: "github.com", Alias: "github-work",
		Key:  "/home/u/.ssh/id_work",
		Dirs: []string{"/home/u/work/"},
		User: User{Name: "A", Email: "a@example.com"},
	}}}

	data, err := f.Marshal("/home/u")
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	out := string(data)

	if !strings.Contains(out, "~/.ssh/id_work") {
		t.Errorf("key was not contracted to ~ form:\n%s", out)
	}
	if strings.Contains(out, "/home/u/") {
		t.Errorf("an absolute path survived:\n%s", out)
	}
}

// Marshal must not mutate the document it is given, or the caller's in-memory
// paths silently become relative.
func TestMarshal_DoesNotMutate(t *testing.T) {
	f := &Profiles{Version: Version, Profiles: []Profile{{
		Name: "work", Host: "github.com", Alias: "github-work",
		Key:  "/home/u/.ssh/id_work",
		Dirs: []string{"/home/u/work/"},
		User: User{Name: "A", Email: "a@example.com"},
	}}}

	if _, err := f.Marshal("/home/u"); err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if f.Profiles[0].Key != "/home/u/.ssh/id_work" {
		t.Errorf("Key was mutated to %q", f.Profiles[0].Key)
	}
	if f.Profiles[0].Dirs[0] != "/home/u/work/" {
		t.Errorf("Dirs was mutated to %q", f.Profiles[0].Dirs[0])
	}
}

func TestMarshal_OmitsUnsetFields(t *testing.T) {
	f := &Profiles{Version: Version, Profiles: []Profile{{
		Name: "personal", Host: "github.com", Alias: "github-personal",
		Key:     "~/.ssh/k",
		User:    User{Name: "A", Email: "a@example.com"},
		Default: true,
	}}}

	data, err := f.Marshal("/home/u")
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	out := string(data)

	if strings.Contains(out, "dirs:") {
		t.Errorf("empty dirs was written:\n%s", out)
	}
	if strings.Contains(out, "options:") {
		t.Errorf("empty options was written:\n%s", out)
	}
	if !strings.Contains(out, "default: true") {
		t.Errorf("default was omitted when set:\n%s", out)
	}
}

// Save then load must give back what went in.
func TestMarshal_RoundTrip(t *testing.T) {
	const home = "/home/u"

	original := &Profiles{Version: Version, Profiles: []Profile{
		{Name: "personal", Host: "github.com", Alias: "github-personal", Key: "/home/u/.ssh/p",
			User: User{Name: "A", Email: "a@example.com"}, Default: true},
		{Name: "work", Host: "github.com", Alias: "github-work", Key: "/home/u/.ssh/w",
			User: User{Name: "B", Email: "b@example.org"}, Dirs: []string{"/home/u/work/"},
			Options: map[string]string{"ServerAliveInterval": "60"}},
	}}

	data, err := original.Marshal(home)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	back, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v:\n%s", err, data)
	}
	if err := back.ExpandPaths(home); err != nil {
		t.Fatalf("ExpandPaths() error = %v", err)
	}
	if err := back.Validate(); err != nil {
		t.Fatalf("round trip produced an invalid document: %v", err)
	}

	if len(back.Profiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(back.Profiles))
	}
	if back.Profiles[1].Key != "/home/u/.ssh/w" {
		t.Errorf("Key = %q", back.Profiles[1].Key)
	}
	if back.Profiles[1].Options["ServerAliveInterval"] != "60" {
		t.Errorf("Options = %v", back.Profiles[1].Options)
	}
	if !back.Profiles[0].Default {
		t.Error("Default was lost")
	}
}

func TestLoadOrNew_MissingFileGivesAnEmptyDocument(t *testing.T) {
	f, err := LoadOrNew(filepath.Join(t.TempDir(), "absent.yaml"), "/home/u")
	if err != nil {
		t.Fatalf("LoadOrNew() error = %v, want nil for a missing file", err)
	}
	if f == nil {
		t.Fatal("LoadOrNew() returned nil")
	}
	if f.Version != Version {
		t.Errorf("Version = %d, want %d", f.Version, Version)
	}
	if len(f.Profiles) != 0 {
		t.Errorf("got %d profiles, want none", len(f.Profiles))
	}
}

func TestLoadOrNew_RealErrorsStillSurface(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "bad.yaml")

	if err := os.WriteFile(path, []byte("version: 1\nprofilez: []\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := LoadOrNew(path, home); err == nil {
		t.Error("LoadOrNew() error = nil, want the parse failure to surface")
	}
}

// init writes a file that declares nothing, and add is called next. Treating
// that as an error made init a trap.
func TestLoadOrNew_EmptyDocumentIsAFreshStart(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "profiles.yaml")

	if err := os.WriteFile(path, []byte("version: 1\n# nothing declared yet\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	f, err := LoadOrNew(path, home)
	if err != nil {
		t.Fatalf("LoadOrNew() error = %v, want nil", err)
	}
	if len(f.Profiles) != 0 {
		t.Errorf("got %d profiles, want none", len(f.Profiles))
	}
}
