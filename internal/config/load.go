package config

import (
	"fmt"
	"os"
	"strings"
)

// Load reads, decodes, validates and expands the profiles file at path.
func Load(path, home string) (*Profiles, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profiles: %w", err)
	}

	f, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.ExpandPaths(home); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return f, nil
}

// ByName returns a pointer into the slice, so mutating it mutates the document.
func (f *Profiles) ByName(name string) (*Profile, bool) {
	for i := range f.Profiles {
		if fold(f.Profiles[i].Name) == fold(name) && name != "" {
			return &f.Profiles[i], true
		}
	}
	return nil, false
}

// DefaultProfile returns the profile marked default. Having none is legal.
func (f *Profiles) DefaultProfile() (*Profile, bool) {
	for i := range f.Profiles {
		if f.Profiles[i].Default {
			return &f.Profiles[i], true
		}
	}
	return nil, false
}

// ForDir returns the profile governing dir, preferring the longest match.
func (f *Profiles) ForDir(dir string) (*Profile, bool) {
	dir = withTrailingSlash(dir)
	best := -1
	bestLen := -1
	for i := range f.Profiles {
		for _, d := range f.Profiles[i].Dirs {
			if strings.HasPrefix(dir, d) && len(d) > bestLen {
				best, bestLen = i, len(d)
			}
		}
	}
	if best >= 0 {
		return &f.Profiles[best], true
	}
	return f.DefaultProfile()
}
