package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Contract rewrites absolute paths under home back to ~ form, so a saved file
// stays readable and portable between machines.
func (f *Profiles) Contract(home string) {
	home = strings.TrimSuffix(filepath.Clean(home), "/")

	for i := range f.Profiles {
		p := &f.Profiles[i]
		p.Key = contract(p.Key, home)
		for j, d := range p.Dirs {
			p.Dirs[j] = contract(d, home)
		}
	}
}

// Marshal renders the document as YAML, with paths contracted to ~ form.
func (f *Profiles) Marshal(home string) ([]byte, error) {
	clone := *f
	clone.Profiles = make([]Profile, len(f.Profiles))
	copy(clone.Profiles, f.Profiles)
	for i := range clone.Profiles {
		clone.Profiles[i].Dirs = append([]string(nil), f.Profiles[i].Dirs...)
	}
	clone.Contract(home)

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&clone); err != nil {
		return nil, fmt.Errorf("render profiles: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("render profiles: %w", err)
	}
	return []byte(b.String()), nil
}

// LoadOrNew returns the profiles file at path, or an empty document when the
// file does not exist yet.
func LoadOrNew(path, home string) (*Profiles, error) {
	f, err := Load(path, home)
	if err == nil {
		return f, nil
	}
	// errors.Is, not os.IsNotExist: Load wraps with %w, and the legacy helper
	// does not unwrap, so it would never match.
	//
	// ErrNoProfiles counts as a fresh start too. The starter file init writes
	// declares nothing, and the caller is about to add the first profile.
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrNoProfiles) {
		return &Profiles{Version: Version}, nil
	}
	return nil, err
}

func contract(path, home string) string {
	if path == home {
		return "~"
	}
	if trimmed, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + trimmed
	}
	return path
}
