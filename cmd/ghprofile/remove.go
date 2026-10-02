package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/Tessie475/ghprofile/internal/apply"
	"github.com/Tessie475/ghprofile/internal/config"
)

func (a *app) cmdRemove(args []string) int {
	fset := a.flags("remove")
	fset.Usage = func() {
		fmt.Fprintln(a.errOut, "Usage: ghprofile remove <name>\n\nDrops an identity from the profiles file. Run ghprofile apply afterwards\nto take its host alias and git rules off your machine.")
	}
	positional, err := parse(fset, args)
	if err != nil {
		return exitMisuse
	}

	f, code := a.load()
	if f == nil {
		return code
	}
	if len(positional) != 1 {
		fset.Usage()
		return exitMisuse
	}

	name := positional[0]
	p, ok := f.ByName(name)
	if !ok {
		return a.fail(fmt.Errorf("%q: %w", name, config.ErrUnknownProfile))
	}

	wasDefault := p.Default
	kept := make([]config.Profile, 0, len(f.Profiles)-1)
	for _, candidate := range f.Profiles {
		if candidate.Name != p.Name {
			kept = append(kept, candidate)
		}
	}
	f.Profiles = kept

	// Something has to govern the global identity, so promote the first
	// survivor rather than leaving none.
	if wasDefault && len(f.Profiles) > 0 {
		f.Profiles[0].Default = true
		fmt.Fprintf(a.out, "%q was the default, so %q is now\n", name, f.Profiles[0].Name)
	}

	if len(f.Profiles) > 0 {
		if err := f.Validate(); err != nil {
			return a.fail(err)
		}
	}

	data, err := f.Marshal(a.home)
	if err != nil {
		return a.fail(err)
	}
	if _, err := apply.NewBackups().Save(a.paths.ProfilesFile); err != nil {
		return a.fail(err)
	}
	if err := apply.WriteAtomic(a.paths.ProfilesFile, data, 0o600); err != nil {
		return a.fail(err)
	}

	identity := a.paths.IdentityFile(name)
	if err := os.Remove(identity); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(a.errOut, "could not remove %s: %v\n", identity, err)
	}

	fmt.Fprintf(a.out, "removed %q\n", name)
	fmt.Fprintf(a.out, "\nits key is left at %s, delete it yourself if you do not want it\n", p.Key)
	fmt.Fprintln(a.out, "next: ghprofile apply")
	return exitOK
}
