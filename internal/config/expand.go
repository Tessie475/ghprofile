package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidPath is returned when a path cannot be resolved to an absolute location.
var ErrInvalidPath = errors.New("invalid path")

// ExpandPaths rewrites ~ prefixes under home and gives every directory a
// trailing slash, which is the form git's gitdir: matching expects.
func (f *Profiles) ExpandPaths(home string) error {
	var errs []error

	for i := range f.Profiles {
		p := &f.Profiles[i]

		key, err := expand(p.Key, home)
		if err != nil {
			errs = append(errs, fmt.Errorf("profile %q: key %q: %w", p.Name, p.Key, err))
		} else {
			p.Key = key
		}

		for j, d := range p.Dirs {
			dir, err := expand(d, home)
			if err != nil {
				errs = append(errs, fmt.Errorf("profile %q: dirs[%d] %q: %w", p.Name, j, d, err))
				continue
			}
			p.Dirs[j] = withTrailingSlash(dir)
		}
	}

	return errors.Join(errs...)
}

func expand(path, home string) (string, error) {
	switch {
	case path == "~":
		return filepath.Clean(home), nil
	case strings.HasPrefix(path, "~/"):
		return filepath.Join(home, path[2:]), nil
	case strings.HasPrefix(path, "~"):
		return "", ErrInvalidPath
	case filepath.IsAbs(path):
		return filepath.Clean(path), nil
	default:
		return "", ErrInvalidPath
	}
}

func withTrailingSlash(p string) string {
	if strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}
