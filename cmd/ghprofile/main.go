// Command ghprofile manages multiple Git identities and their SSH keys.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/Tessie475/ghprofile/internal/paths"
)

// Build information, set by the linker at release time. Users have to be able
// to say which build they are on when reporting a problem.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Exit codes are part of the tool's contract with scripts and CI.
const (
	exitOK     = 0
	exitError  = 1
	exitDrift  = 2
	exitMisuse = 3
)

const usage = `ghprofile manages multiple Git identities and their SSH keys.

Usage:
  ghprofile <command> [arguments] [flags]

Getting started, two commands:
  ghprofile add personal --email you@gmail.com --default
  ghprofile add work --email you@company.com --dir ~/company/
  ghprofile apply

In "add work", the word "work" is a label you choose for one identity. It names
the profile and builds its ssh alias, so "work" gives you github-work. Use
whatever describes the account to you.

Commands:
  add <name>     Add an identity. <name> is yours to invent, e.g. work
  apply          Do everything: keys, ssh config, git rules, key upload, checks
  remove <name>  Drop an identity from the profiles file
  default <name> Choose which identity applies everywhere else
  show           List the identities you have declared
  plan           Show what apply would change, without changing it
  check          Inspect your setup and report problems, offline
  verify         Ask each host which account its key reaches, online
  keys           Ask which account every key in ~/.ssh reaches
  upload <name>  Re-run just the clipboard and browser handoff for one identity
  adopt          Take over hand-written host stanzas
  fix-remote     Rewrite a repository's remote to the right host alias
  init           Write a starter profiles file to edit by hand instead
  version        Print the build you are running

Run ghprofile <command> -h for the flags of a single command.
`

type app struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	home   string
	paths  paths.Paths
	darwin bool
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run returns an exit code so that tests can call it without exiting.
func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(errOut, usage)
		return exitMisuse
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(errOut, "ghprofile: %v\n", err)
		return exitError
	}
	if h := os.Getenv("GHPROFILE_HOME"); h != "" {
		home = h
	}

	a := &app{
		in:     in,
		out:    out,
		errOut: errOut,
		home:   home,
		paths:  paths.Default(home),
		darwin: runtime.GOOS == "darwin",
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(out, usage)
		return exitOK
	case "version", "-v", "--version":
		fmt.Fprintf(out, "ghprofile %s\ncommit  %s\nbuilt   %s\n%s/%s\n",
			version, commit, date, runtime.GOOS, runtime.GOARCH)
		return exitOK
	case "default":
		return a.cmdDefault(args[1:])
	case "remove", "rm":
		return a.cmdRemove(args[1:])
	case "add":
		return a.cmdAdd(ctx, args[1:])
	case "init":
		return a.cmdInit(args[1:])
	case "show":
		return a.cmdShow(args[1:])
	case "plan":
		return a.cmdPlan(args[1:])
	case "apply":
		return a.cmdApply(ctx, args[1:])
	// doctor is the brew and flutter convention, kept working but undocumented.
	case "keys":
		return a.cmdKeys(ctx, args[1:])
	case "check", "doctor":
		return a.cmdCheck(ctx, args[1:])
	case "adopt":
		return a.cmdAdopt(args[1:])
	case "upload":
		return a.cmdUpload(ctx, args[1:])
	case "verify":
		return a.cmdVerify(ctx, args[1:])
	case "fix-remote":
		return a.cmdFixRemote(ctx, args[1:])
	default:
		fmt.Fprintf(errOut, "ghprofile: unknown command %q\n\n%s", args[0], usage)
		return exitMisuse
	}
}

func (a *app) fail(err error) int {
	fmt.Fprintf(a.errOut, "ghprofile: %v\n", err)
	return exitError
}
