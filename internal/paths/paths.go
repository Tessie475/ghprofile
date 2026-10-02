// Package paths resolves the filesystem locations ghprofile reads and writes.
package paths

import "path/filepath"

// Paths holds every location the tool touches, rooted at a home directory.
type Paths struct {
	Home         string
	SSHDir       string
	SSHConfig    string
	GitConfig    string
	ConfigDir    string
	ProfilesFile string
}

// Default returns the standard locations under home.
func Default(home string) Paths {
	cfg := filepath.Join(home, ".config", "ghprofile")
	return Paths{
		Home:         home,
		SSHDir:       filepath.Join(home, ".ssh"),
		SSHConfig:    filepath.Join(home, ".ssh", "config"),
		GitConfig:    filepath.Join(home, ".gitconfig"),
		ConfigDir:    cfg,
		ProfilesFile: filepath.Join(cfg, "profiles.yaml"),
	}
}

// IdentityFile returns the git identity include written for a profile.
func (p Paths) IdentityFile(profile string) string {
	return filepath.Join(p.ConfigDir, "gitconfig-"+profile)
}
