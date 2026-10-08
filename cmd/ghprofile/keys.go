package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/keys"
	"github.com/Tessie475/ghprofile/internal/verify"
)

func (a *app) cmdKeys(ctx context.Context, args []string) int {
	fset := a.flags("keys")
	fset.Usage = func() {
		fmt.Fprintln(a.errOut, "Usage: ghprofile keys [-host <host>]\n\nAsks each host which account every key in ~/.ssh reaches. A key's comment is\nwhatever was typed when it was made, so it proves nothing; this asks the\nserver instead.")
	}
	host := fset.String("host", "", "check against this host only")
	timeout := fset.Duration("timeout", 15*time.Second, "how long to wait for each attempt")
	if _, err := parse(fset, args); err != nil {
		return exitMisuse
	}

	found, err := keys.Existing(a.paths.SSHDir)
	if err != nil {
		return a.fail(err)
	}
	if len(found) == 0 {
		fmt.Fprintf(a.out, "no keypairs in %s\n", a.paths.SSHDir)
		return exitOK
	}

	// Profiles are optional here: the point is to help someone decide what to
	// declare, which they cannot have declared yet.
	f, _ := config.LoadOrNew(a.paths.ProfilesFile, a.home)
	hosts, owner := a.survey(f, *host)

	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tHOST\tACCOUNT\tPROFILE")

	checker := verify.New()
	for _, k := range found {
		for _, h := range hosts {
			c, cancel := context.WithTimeout(ctx, *timeout)
			res, err := checker.Key(c, k.Path, h)
			cancel()

			account := "no access"
			switch {
			case err != nil:
				account = "error: " + firstLine(err.Error())
			case res.Authenticated:
				account = res.Username
			}
			claimed := owner[k.Path]
			if claimed == "" {
				claimed = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.short(k.Path), h, account, claimed)
		}
	}

	if err := w.Flush(); err != nil {
		return a.fail(err)
	}
	fmt.Fprintln(a.out, "\na key reaching an account you want is usable as is: ghprofile add <name> -key <path>")
	return exitOK
}

// survey returns the hosts worth asking and which profile already claims each
// key, so the table says what is already spoken for.
func (a *app) survey(f *config.Profiles, only string) ([]string, map[string]string) {
	owner := map[string]string{}
	seen := map[string]bool{}
	var hosts []string

	for i := range f.Profiles {
		p := &f.Profiles[i]
		owner[p.Key] = p.Name
		if !seen[p.Host] {
			seen[p.Host] = true
			hosts = append(hosts, p.Host)
		}
	}

	switch {
	case only != "":
		hosts = []string{only}
	case len(hosts) == 0:
		hosts = []string{"github.com"}
	default:
		sort.Strings(hosts)
	}

	return hosts, owner
}

// short contracts a path back to ~ form so the table stays readable.
func (a *app) short(path string) string {
	if trimmed, ok := strings.CutPrefix(path, a.home+"/"); ok {
		return "~/" + trimmed
	}
	return path
}
