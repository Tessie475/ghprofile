package config

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Version is the only profiles file version this build supports.
const Version = 1

// Validation failures. Callers identify them with errors.Is.
var (
	ErrUnsupportedVersion = errors.New("unsupported profiles version")
	ErrNoProfiles         = errors.New("no profiles defined")
	ErrMissingField       = errors.New("missing required field")
	ErrInvalidAlias       = errors.New("invalid host alias")
	ErrInvalidEmail       = errors.New("invalid email address")
	ErrDuplicateName      = errors.New("duplicate profile name")
	ErrDuplicateAlias     = errors.New("duplicate host alias")
	ErrDuplicateDir       = errors.New("duplicate directory")
	ErrDuplicateKey       = errors.New("two profiles share a private key")
	ErrMultipleDefaults   = errors.New("more than one default profile")
	ErrUnknownProfile     = errors.New("unknown profile")
)

// Validate reports every problem with the document, not only the first.
func (f *Profiles) Validate() error {
	var errs []error

	if f.Version != Version {
		errs = append(errs, fmt.Errorf("version %d: %w", f.Version, ErrUnsupportedVersion))
	}
	if len(f.Profiles) == 0 {
		errs = append(errs, ErrNoProfiles)
	}

	names := make(map[string]int, len(f.Profiles))
	profileKeys := make(map[string]int, len(f.Profiles))
	aliases := make(map[string]int, len(f.Profiles))
	dirs := make(map[string]int)
	defaults := 0

	for i, p := range f.Profiles {
		if err := p.Validate(); err != nil {
			errs = append(errs, err)
		}
		if p.Default {
			defaults++
		}

		if k := fold(p.Name); k != "" {
			if j, seen := names[k]; seen {
				errs = append(errs, fmt.Errorf("profiles[%d] and profiles[%d]: name %q: %w", j, i, p.Name, ErrDuplicateName))
			} else {
				names[k] = i
			}
		}
		if k := fold(p.Alias); k != "" {
			if j, seen := aliases[k]; seen {
				errs = append(errs, fmt.Errorf("profiles[%d] and profiles[%d]: alias %q: %w", j, i, p.Alias, ErrDuplicateAlias))
			} else {
				aliases[k] = i
			}
		}
		if k := strings.TrimSpace(p.Key); k != "" {
			if j, seen := profileKeys[k]; seen {
				errs = append(errs, fmt.Errorf("profiles[%d] and profiles[%d]: key %q: %w", j, i, p.Key, ErrDuplicateKey))
			} else {
				profileKeys[k] = i
			}
		}
		for _, d := range p.Dirs {
			k := strings.TrimSuffix(strings.TrimSpace(d), "/")
			if k == "" {
				continue
			}
			if j, seen := dirs[k]; seen {
				errs = append(errs, fmt.Errorf("profiles[%d] and profiles[%d]: dir %q: %w", j, i, d, ErrDuplicateDir))
			} else {
				dirs[k] = i
			}
		}
	}

	if defaults > 1 {
		errs = append(errs, fmt.Errorf("%d profiles marked default: %w", defaults, ErrMultipleDefaults))
	}

	return errors.Join(errs...)
}

// Validate reports every problem with a single profile, ignoring collisions
// with other profiles.
func (p Profile) Validate() error {
	var errs []error

	required := []struct{ field, value string }{
		{"name", p.Name},
		{"host", p.Host},
		{"alias", p.Alias},
		{"key", p.Key},
		{"user.name", p.User.Name},
		{"user.email", p.User.Email},
	}
	for _, r := range required {
		if strings.TrimSpace(r.value) == "" {
			errs = append(errs, fmt.Errorf("profile %q: %s: %w", p.Name, r.field, ErrMissingField))
		}
	}

	if a := strings.TrimSpace(p.Alias); a != "" && !validAlias(a) {
		errs = append(errs, fmt.Errorf("profile %q: alias %q: %w", p.Name, p.Alias, ErrInvalidAlias))
	}
	if e := strings.TrimSpace(p.User.Email); e != "" && !validEmail(e) {
		errs = append(errs, fmt.Errorf("profile %q: email %q: %w", p.Name, p.User.Email, ErrInvalidEmail))
	}
	for i, d := range p.Dirs {
		if strings.TrimSpace(d) == "" {
			errs = append(errs, fmt.Errorf("profile %q: dirs[%d]: %w", p.Name, i, ErrMissingField))
		}
	}
	for k, v := range p.Options {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			errs = append(errs, fmt.Errorf("profile %q: options[%q]: %w", p.Name, k, ErrMissingField))
		}
	}

	return errors.Join(errs...)
}

func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// validAlias rejects the characters OpenSSH treats as wildcards in a Host pattern.
func validAlias(s string) bool {
	if strings.ContainsAny(s, "*?") {
		return false
	}
	return strings.IndexFunc(s, unicode.IsSpace) < 0
}

// validEmail catches typos rather than enforcing RFC 5322.
func validEmail(s string) bool {
	if strings.IndexFunc(s, unicode.IsSpace) >= 0 {
		return false
	}
	at := strings.Index(s, "@")
	return at > 0 && at == strings.LastIndex(s, "@") && at < len(s)-1
}
