package apply

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tessie475/ghprofile/internal/plan"
)

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	if err := WriteAtomic(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("content = %q", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteAtomic_LeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()

	if err := WriteAtomic(filepath.Join(dir, "config"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ghprofile-") {
			t.Errorf("temp file %q was left behind", e.Name())
		}
	}
}

func TestBackups_Save(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewBackups()
	dest, err := b.Save(path)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if dest == "" {
		t.Fatal("Save() returned no path for an existing file")
	}
	if got := readFile(t, dest); got != "original\n" {
		t.Errorf("backup holds %q", got)
	}
	if !strings.Contains(dest, ".ghprofile-backup-") {
		t.Errorf("backup name %q does not identify itself", dest)
	}
}

func TestBackups_MissingFileIsNotAnError(t *testing.T) {
	dest, err := NewBackups().Save(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	if dest != "" {
		t.Errorf("Save() = %q, want empty for a file that does not exist", dest)
	}
}

// The regression test for real data loss. A single run writes the ssh config
// twice: once to take over a hand-written stanza, once to add the managed
// block. Both happen in the same second. An earlier version named backups with
// second precision and copied on every write, so the second copy captured the
// already-modified file AND overwrote the first, destroying the only record of
// the original.
func TestBackups_SaveKeepsThePreRunStateAcrossRepeatedWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	if err := os.WriteFile(path, []byte("ORIGINAL\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewBackups()

	first, err := b.Save(path)
	if err != nil {
		t.Fatalf("first Save() error = %v", err)
	}

	// Something modifies the file, as take-over does.
	if err := os.WriteFile(path, []byte("MODIFIED\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	second, err := b.Save(path)
	if err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	if second != first {
		t.Errorf("second Save() wrote a new backup %q, want the first one %q reused", second, first)
	}
	if got := readFile(t, first); got != "ORIGINAL\n" {
		t.Errorf("backup holds %q, want the pre-run state ORIGINAL", got)
	}

	// And exactly one backup exists, not two.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".ghprofile-backup-") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("found %d backups, want exactly 1 per file per run", count)
	}
}

// Two separate runs in the same second must not clobber each other either.
func TestBackups_SaveNeverOverwritesAnExistingBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	frozen := func() time.Time { return time.Unix(1700000000, 0).UTC() }

	if err := os.WriteFile(path, []byte("RUN ONE\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	runOne := &Backups{done: map[string]string{}, clock: frozen}
	firstDest, err := runOne.Save(path)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := os.WriteFile(path, []byte("RUN TWO\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	runTwo := &Backups{done: map[string]string{}, clock: frozen}
	secondDest, err := runTwo.Save(path)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if secondDest == firstDest {
		t.Fatal("the second run reused the first run's backup name and destroyed it")
	}
	if got := readFile(t, firstDest); got != "RUN ONE\n" {
		t.Errorf("the first run's backup now holds %q", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestRun_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	actions := []plan.Action{{
		Kind: plan.WriteFile, Path: path, Mode: 0o600, Content: "new\n",
	}}

	var out bytes.Buffer
	if err := Run(context.Background(), actions, Options{DryRun: true}, &out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("dry run created the file")
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("dry run did not report the action:\n%s", out.String())
	}
}

func TestRun_BacksUpBeforeOverwriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	actions := []plan.Action{{Kind: plan.WriteFile, Path: path, Mode: 0o600, Content: "replaced\n"}}

	var out bytes.Buffer
	if err := Run(context.Background(), actions, Options{Backups: NewBackups()}, &out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "replaced\n" {
		t.Fatalf("file was not replaced: %q %v", got, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var found bool
	for _, e := range entries {
		if strings.Contains(e.Name(), ".ghprofile-backup-") {
			found = true
			data, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
			if readErr != nil || string(data) != "original\n" {
				t.Errorf("backup holds %q, want the original", data)
			}
		}
	}
	if !found {
		t.Error("no backup was written")
	}
}

func TestRun_MakeDirUsesTightPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ssh")

	actions := []plan.Action{{Kind: plan.MakeDir, Path: dir, Mode: fs.FileMode(0o700)}}

	var out bytes.Buffer
	if err := Run(context.Background(), actions, Options{}, &out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want 0700", info.Mode().Perm())
	}
}

// The backup name must be claimed and created in one step. An earlier version
// checked with Lstat and wrote afterwards, which acted on a stale observation.
// This asserts the observable consequence: a name already taken is never
// overwritten, even when the clock is frozen so every run wants the same name.
func TestBackups_SaveNeverOverwritesEvenWithAFrozenClock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	frozen := func() time.Time { return time.Unix(1700000000, 0).UTC() }

	var written []string
	for i, body := range []string{"RUN ONE\n", "RUN TWO\n", "RUN THREE\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("setup %d: %v", i, err)
		}
		dest, err := (&Backups{done: map[string]string{}, clock: frozen}).Save(path)
		if err != nil {
			t.Fatalf("Save() %d error = %v", i, err)
		}
		written = append(written, dest)
	}

	// Three distinct names, despite one timestamp.
	seen := map[string]bool{}
	for _, d := range written {
		if seen[d] {
			t.Fatalf("name %q was reused, so an earlier backup was destroyed", d)
		}
		seen[d] = true
	}

	// And each still holds what it captured.
	for i, want := range []string{"RUN ONE\n", "RUN TWO\n", "RUN THREE\n"} {
		if got := readFile(t, written[i]); got != want {
			t.Errorf("backup %d holds %q, want %q", i, got, want)
		}
	}
}

// O_CREATE leaves the mode subject to the umask, so it is set explicitly.
func TestBackups_SavePreservesMode(t *testing.T) {
	dir := t.TempDir()

	for _, mode := range []fs.FileMode{0o600, 0o644} {
		path := filepath.Join(dir, fmt.Sprintf("config-%o", mode))
		if err := os.WriteFile(path, []byte("x\n"), mode); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("setup chmod: %v", err)
		}

		dest, err := NewBackups().Save(path)
		if err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		info, err := os.Stat(dest)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("backup of a %v file is %v, want %v", mode, info.Mode().Perm(), mode)
		}
	}
}
