package apply

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// Backups records which files have already been copied during one run.
//
// A single command can write the same file more than once, for example taking
// over a hand-written stanza and then writing the managed block. Without this,
// the second copy would capture an already-modified file, and because the
// names carry only second precision it would also overwrite the first. The
// only record of the original state would be gone.
type Backups struct {
	done  map[string]string
	clock func() time.Time
}

// NewBackups starts a fresh record for one run.
func NewBackups() *Backups {
	return &Backups{done: map[string]string{}, clock: time.Now}
}

// Save copies path next to itself with a timestamped suffix, once per run.
// Calling it again for the same path returns the first backup's name and
// copies nothing, so the backup always holds the pre-run state.
//
// A file that does not exist yet needs no backup, and returns an empty name.
func (b *Backups) Save(path string) (string, error) {
	if b == nil {
		return "", nil
	}
	if dest, already := b.done[path]; already {
		return dest, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		b.done[path] = ""
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}

	dest, err := b.reserve(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, data, info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("write %s: %w", dest, err)
	}

	b.done[path] = dest
	return dest, nil
}

// reserve picks a name no existing file holds, so a second run in the same
// second cannot clobber an earlier backup either.
func (b *Backups) reserve(path string) (string, error) {
	stamp := b.clock().UTC().Format("20060102T150405Z")
	base := fmt.Sprintf("%s.ghprofile-backup-%s", path, stamp)

	for i := 0; i < 100; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", base, i)
		}
		if _, err := os.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("stat %s: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("could not find an unused backup name for %s", path)
}
