package main

import (
	"bufio"
	"fmt"
	"strings"
)

const claimWarning = `
Claiming "Host %s" changes how plain URLs behave. A clone like

    git clone git@%s:owner/repo.git

will then offer ONLY the %q key, because the stanza carries IdentitiesOnly yes.

  - Repositories that use an alias, git@%s:owner/repo.git, are unaffected.
  - Any repository belonging to a DIFFERENT account that still uses a plain
    git@%s: URL will stop authenticating until you point it at that account's
    alias. "ghprofile fix-remote -all" does that for you.

Without this, a plain URL offers whatever key your ssh agent happens to hold,
which usually works and silently changes after a reboot.

`

// confirmClaim explains what claiming the bare hostname breaks and waits for
// an explicit yes. Anything other than y or yes is a no.
func (a *app) confirmClaim(profile, alias, host string, assumeYes bool) bool {
	fmt.Fprintf(a.out, claimWarning, host, host, profile, alias, host)

	if assumeYes {
		fmt.Fprintln(a.out, "continuing, -yes was given")
		return true
	}

	fmt.Fprint(a.out, "Continue? [y/N]: ")
	line, err := bufio.NewReader(a.in).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		fmt.Fprintln(a.errOut, "\nno answer, leaving the hostname unclaimed. pass -yes to skip this prompt.")
		return false
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		fmt.Fprintln(a.out, "leaving the hostname unclaimed")
		return false
	}
}
