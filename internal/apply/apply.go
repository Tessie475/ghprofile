// Package apply carries out the actions a plan produced.
package apply

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Tessie475/ghprofile/internal/keys"
	"github.com/Tessie475/ghprofile/internal/plan"
)

// Options controls how far Run is allowed to go.
//
// Backups is the per-run record of files already copied. Leave it nil to skip
// backups entirely.
type Options struct {
	DryRun  bool
	Backups *Backups
}

// Run executes each action in order, stopping at the first failure.
func Run(ctx context.Context, actions []plan.Action, opts Options, out io.Writer) error {
	for _, a := range actions {
		fmt.Fprintf(out, "%s\n", a)

		if opts.DryRun {
			continue
		}
		if err := execute(ctx, a, opts); err != nil {
			return fmt.Errorf("%s %s: %w", a.Kind, a.Path, err)
		}
	}
	return nil
}

func execute(ctx context.Context, a plan.Action, opts Options) error {
	switch a.Kind {
	case plan.MakeDir:
		return os.MkdirAll(a.Path, a.Mode)

	case plan.MakeKey:
		if err := os.MkdirAll(filepath.Dir(a.Path), keys.DirMode); err != nil {
			return err
		}
		return keys.Generate(ctx, a.Path, a.Comment)

	case plan.Chmod:
		return os.Chmod(a.Path, a.Mode)

	case plan.WriteFile:
		if _, err := opts.Backups.Save(a.Path); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(a.Path), keys.DirMode); err != nil {
			return err
		}
		return WriteAtomic(a.Path, []byte(a.Content), a.Mode)

	default:
		return fmt.Errorf("unknown action kind %q", a.Kind)
	}
}

// WriteAtomic replaces a file in one step, so an interrupted write cannot
// leave a half-written config behind.
func WriteAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".ghprofile-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	return os.Rename(tmpName, path)
}
