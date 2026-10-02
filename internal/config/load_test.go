package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "profiles.yaml")

	if err := os.WriteFile(path, []byte(validDoc), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	f, err := Load(path, home)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantKey := filepath.Join(home, ".ssh", "personal_github")
	if f.Profiles[0].Key != wantKey {
		t.Errorf("Key = %q, want %q", f.Profiles[0].Key, wantKey)
	}
	wantDir := filepath.Join(home, "work") + "/"
	if f.Profiles[1].Dirs[0] != wantDir {
		t.Errorf("Dirs[0] = %q, want %q", f.Profiles[1].Dirs[0], wantDir)
	}
}

func TestLoad_Errors(t *testing.T) {
	home := t.TempDir()

	t.Run("missing file", func(t *testing.T) {
		_, err := Load(filepath.Join(home, "nope.yaml"), home)
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("error = %v, want it to wrap os.ErrNotExist", err)
		}
	})

	t.Run("invalid document", func(t *testing.T) {
		path := filepath.Join(home, "bad.yaml")
		bad := strings.Replace(validDoc, "github-work", "github-personal", 1)
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if _, err := Load(path, home); !errors.Is(err, ErrDuplicateAlias) {
			t.Errorf("error = %v, want it to wrap %v", err, ErrDuplicateAlias)
		}
	})
}

func TestProfiles_Lookups(t *testing.T) {
	f := goodFile()

	p, ok := f.ByName("work")
	if !ok {
		t.Fatal("ByName(work) not found")
	}
	p.Host = "changed"
	if f.Profiles[1].Host != "changed" {
		t.Error("ByName returned a copy, it must point into the slice")
	}
	if _, ok := f.ByName(""); ok {
		t.Error("ByName(empty) reported found")
	}

	d, ok := f.DefaultProfile()
	if !ok || d.Name != "personal" {
		t.Errorf("DefaultProfile() = %v, %v, want personal", d, ok)
	}
}

func TestProfiles_ForDir(t *testing.T) {
	f := goodFile()
	f.Profiles[1].Dirs = []string{"/home/u/work/"}

	got, ok := f.ForDir("/home/u/work/repo")
	if !ok || got.Name != "work" {
		t.Errorf("ForDir(work repo) = %v, want work", got)
	}

	got, ok = f.ForDir("/home/u/elsewhere")
	if !ok || got.Name != "personal" {
		t.Errorf("ForDir(unmatched) = %v, want the default profile", got)
	}
}
