package main

import (
	"fmt"

	"github.com/Tessie475/ghprofile/internal/apply"
	"github.com/Tessie475/ghprofile/internal/config"
)

func (a *app) cmdDefault(args []string) int {
	fset := a.flags("default")
	claimHost := fset.Bool("claim-host", false, "also make plain git@<host> URLs use this identity (read the warning)")
	noClaimHost := fset.Bool("no-claim-host", false, "stop this identity answering plain git@<host> URLs")
	assumeYes := fset.Bool("yes", false, "do not prompt to confirm -claim-host")
	fset.Usage = func() {
		fmt.Fprintln(a.errOut, "Usage: ghprofile default <name>\n\nMakes <name> the identity used anywhere no other profile's directories match,\nand the one a bare git@<host>:owner/repo URL resolves to. Run ghprofile apply\nafterwards. With no argument it prints the current default.")
	}
	positional, err := parse(fset, args)
	if err != nil {
		return exitMisuse
	}

	f, code := a.load()
	if f == nil {
		return code
	}

	if len(positional) == 0 {
		d, ok := f.DefaultProfile()
		if !ok {
			fmt.Fprintln(a.out, "no profile is marked default")
			return exitOK
		}
		claim := "a plain git@" + d.Host + " URL uses whatever your ssh agent offers"
		if d.ClaimHost {
			claim = "also answers a plain git@" + d.Host + " URL"
		}
		fmt.Fprintf(a.out, "%s  (%s)\n%s\n", d.Name, d.User.Email, claim)
		return exitOK
	}

	name := positional[0]
	target, ok := f.ByName(name)
	if !ok {
		return a.fail(fmt.Errorf("%q: %w", name, config.ErrUnknownProfile))
	}
	if *claimHost && *noClaimHost {
		fmt.Fprintln(a.errOut, "ghprofile: -claim-host and -no-claim-host contradict each other")
		return exitMisuse
	}
	if target.Default && !*claimHost && !*noClaimHost {
		fmt.Fprintf(a.out, "%q is already the default\n", target.Name)
		return exitOK
	}

	previous := ""
	if d, had := f.DefaultProfile(); had {
		previous = d.Name
	}
	for i := range f.Profiles {
		f.Profiles[i].Default = f.Profiles[i].Name == target.Name
	}

	switch {
	case *noClaimHost:
		target.ClaimHost = false
	case *claimHost:
		target.ClaimHost = a.confirmClaim(target.Name, target.Alias, target.Host, *assumeYes)
	}

	if err := f.Validate(); err != nil {
		return a.fail(err)
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

	switch {
	case previous == target.Name:
		fmt.Fprintf(a.out, "%q is still the default\n", target.Name)
	case previous != "":
		fmt.Fprintf(a.out, "default moved from %q to %q\n", previous, target.Name)
	default:
		fmt.Fprintf(a.out, "%q is now the default\n", target.Name)
	}
	switch {
	case target.ClaimHost:
		fmt.Fprintf(a.out, "\na plain git@%s URL will now use %s\n", target.Host, target.User.Email)
	case *noClaimHost:
		fmt.Fprintf(a.out, "\na plain git@%s URL will go back to whichever key your agent offers\n", target.Host)
	}
	fmt.Fprintln(a.out, "next: ghprofile apply")
	return exitOK
}
