package plan

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/keys"
	"github.com/Tessie475/ghprofile/internal/paths"
)

// ReadState gathers the current machine state. This is the only part of the
// package that touches the filesystem.
func ReadState(f *config.Profiles, pa paths.Paths) (State, error) {
	st := State{
		Identities: map[string]string{},
		Keys:       map[string]keys.Key{},
		Dirs:       map[string]bool{},
	}

	var err error
	if st.SSHConfig, err = readIfExists(pa.SSHConfig); err != nil {
		return st, err
	}
	if st.GitConfig, err = readIfExists(pa.GitConfig); err != nil {
		return st, err
	}

	for _, dir := range []string{pa.SSHDir, pa.ConfigDir} {
		info, statErr := os.Stat(dir)
		st.Dirs[dir] = statErr == nil && info.IsDir()
	}

	for _, p := range f.Profiles {
		k, inspectErr := keys.Inspect(p.Key)
		if inspectErr != nil {
			return st, inspectErr
		}
		st.Keys[p.Key] = k

		if st.Identities[p.Name], err = readIfExists(pa.IdentityFile(p.Name)); err != nil {
			return st, err
		}
	}

	return st, nil
}

func readIfExists(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}
