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

	dest, err := b.write(path, data, info.Mode().Perm())
	if err != nil {
		return "", err
	}

	b.done[path] = dest
	return dest, nil
}

// write creates the backup under the first name nothing else holds, so a
// second run in the same second cannot clobber an earlier backup.
//
// O_EXCL makes claiming the name and creating the file one step. Checking with
// Lstat first and writing afterwards would leave a window in which another
// process could take the name between the two, and acting on a stale
// observation is worse than not looking at all.
func (b *Backups) write(path string, data []byte, perm fs.FileMode) (string, error) {
	stamp := b.clock().UTC().Format("20060102T150405Z")
	base := fmt.Sprintf("%s.ghprofile-backup-%s", path, stamp)

	for i := 0; i < 100; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", base, i)
		}

		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create %s: %w", candidate, err)
		}

		if err := finish(f, data, perm); err != nil {
			return "", fmt.Errorf("write %s: %w", candidate, err)
		}
		return candidate, nil
	}
	return "", fmt.Errorf("could not find an unused backup name for %s", path)
}

// finish writes the body and fixes the mode, which O_CREATE leaves subject to
// the process umask.
func finish(f *os.File, data []byte, perm fs.FileMode) error {
	defer func() { _ = f.Close() }()

	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return err
	}
	return f.Close()
}
