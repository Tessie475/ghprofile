package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeHome builds an isolated home directory and points the CLI at it.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GHPROFILE_HOME", home)
	return home
}

func exec2(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, strings.NewReader("\n"), &out, &errOut)
	return code, out.String(), errOut.String()
}

func writeProfiles(t *testing.T, home string) {
	t.Helper()

	doc := `version: 1
profiles:
  - name: personal
    host: github.com
    alias: github-personal
    key: ~/.ssh/id_personal
    user:
      name: A Person
      email: a@example.com
    default: true

  - name: work
    host: github.com
    alias: github-work
    key: ~/.ssh/id_work
    user:
      name: A Person
      email: a@work.example.com
    dirs:
      - ~/work/
`
	dir := filepath.Join(home, ".config", "ghprofile")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles.yaml"), []byte(doc), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func TestRun_NoArgs(t *testing.T) {
	if code, _, _ := exec2(t); code != exitMisuse {
		t.Errorf("exit = %d, want %d", code, exitMisuse)
	}
}

func TestRun_Help(t *testing.T) {
	code, out, _ := exec2(t, "--help")
	if code != exitOK {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "ghprofile") {
		t.Error("help output does not mention the tool")
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	if code, _, _ := exec2(t, "wat"); code != exitMisuse {
		t.Errorf("exit = %d, want %d", code, exitMisuse)
	}
}

func TestInitAndShow(t *testing.T) {
	home := fakeHome(t)

	if code, _, errOut := exec2(t, "init"); code != exitOK {
		t.Fatalf("init exit = %d: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ghprofile", "profiles.yaml")); err != nil {
		t.Fatalf("init wrote no profiles file: %v", err)
	}

	// A second init must refuse rather than clobber.
	if code, _, _ := exec2(t, "init"); code != exitError {
		t.Errorf("second init exit = %d, want %d", code, exitError)
	}

	code, out, errOut := exec2(t, "show")
	if code != exitOK {
		t.Fatalf("show exit = %d: %s", code, errOut)
	}
	if !strings.Contains(out, "github-personal") {
		t.Errorf("show output missing the alias:\n%s", out)
	}
}

func TestPlan_ReportsDriftAndWritesNothing(t *testing.T) {
	home := fakeHome(t)
	writeProfiles(t, home)

	code, out, errOut := exec2(t, "plan")
	if code != exitOK {
		t.Fatalf("plan exit = %d: %s", code, errOut)
	}
	if !strings.Contains(out, "change(s) pending") {
		t.Errorf("plan reported no drift on an empty home:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "config")); !os.IsNotExist(err) {
		t.Error("plan wrote an ssh config")
	}

	if code, _, _ := exec2(t, "plan", "-check"); code != exitDrift {
		t.Errorf("plan -check exit = %d, want %d", code, exitDrift)
	}
}

func TestApply_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("generates real keys with ssh-keygen")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}

	home := fakeHome(t)
	writeProfiles(t, home)

	// An existing hand-written ssh config that must survive untouched.
	const foreign = "Host bastion\n  HostName bastion.example.com\n  ProxyJump none\n"
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(foreign), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[pull]\n\trebase = true\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if code, _, errOut := exec2(t, "apply", "-no-upload"); code != exitOK {
		t.Fatalf("apply exit = %d: %s", code, errOut)
	}

	sshCfg := readFile(t, filepath.Join(home, ".ssh", "config"))
	if !strings.Contains(sshCfg, "Host bastion") || !strings.Contains(sshCfg, "ProxyJump none") {
		t.Errorf("apply destroyed hand-written config:\n%s", sshCfg)
	}
	for _, want := range []string{"Host github-work", "IdentitiesOnly yes", "# BEGIN ghprofile:work"} {
		if !strings.Contains(sshCfg, want) {
			t.Errorf("ssh config missing %q:\n%s", want, sshCfg)
		}
	}

	gitCfg := readFile(t, filepath.Join(home, ".gitconfig"))
	if !strings.Contains(gitCfg, "rebase = true") {
		t.Errorf("apply destroyed the existing gitconfig:\n%s", gitCfg)
	}
	if !strings.Contains(gitCfg, "gitdir:"+filepath.Join(home, "work")+"/") {
		t.Errorf("gitconfig missing the conditional include:\n%s", gitCfg)
	}

	identity := readFile(t, filepath.Join(home, ".config", "ghprofile", "gitconfig-work"))
	if !strings.Contains(identity, "a@work.example.com") {
		t.Errorf("work identity file is wrong:\n%s", identity)
	}

	for _, name := range []string{"id_personal", "id_work"} {
		path := filepath.Join(home, ".ssh", name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("key %s was not created: %v", name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", name, info.Mode().Perm())
		}
		if _, err := os.Stat(path + ".pub"); err != nil {
			t.Errorf("%s.pub was not created", name)
		}
	}

	// The whole point: running it again must be a no-op.
	code, out, _ := exec2(t, "plan")
	if code != exitOK {
		t.Fatalf("second plan exit = %d", code)
	}
	if !strings.Contains(out, "up to date") {
		t.Errorf("apply is not idempotent, second plan says:\n%s", out)
	}

	before := sshCfg
	if code, _, _ := exec2(t, "apply", "-no-upload"); code != exitOK {
		t.Fatal("second apply failed")
	}
	if after := readFile(t, filepath.Join(home, ".ssh", "config")); after != before {
		t.Errorf("second apply changed the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestCheck_FindsOrphanedKey(t *testing.T) {
	home := fakeHome(t)
	writeProfiles(t, home)

	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	orphan := filepath.Join(home, ".ssh", "id_stray.pub")
	if err := os.WriteFile(orphan, []byte("ssh-ed25519 AAAA test\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	code, out, errOut := exec2(t, "check")
	if code != exitOK {
		t.Fatalf("check exit = %d: %s", code, errOut)
	}
	if !strings.Contains(out, "id_stray.pub") {
		t.Errorf("check missed the orphaned public key:\n%s", out)
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

// apply must not reach the network unless it is going to help you upload a
// key. An earlier version verified unconditionally, which made this test suite
// make real authenticated calls to GitHub.
func TestApply_NoUploadStaysOffline(t *testing.T) {
	home := fakeHome(t)
	writeProfiles(t, home)

	start := time.Now()
	code, out, errOut := exec2(t, "apply", "-dry-run", "-no-upload")
	elapsed := time.Since(start)

	if code != exitOK {
		t.Fatalf("exit = %d: %s", code, errOut)
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %s, which suggests it opened a network connection", elapsed)
	}
	if strings.Contains(out, "authenticates as") {
		t.Errorf("dry run attempted verification:\n%s", out)
	}
}

// Go's flag package stops at the first positional argument, so flags written
// after the profile name were silently ignored and add fell back to prompting.
func TestParse_FlagsAfterPositional(t *testing.T) {
	fset := flag.NewFlagSet("t", flag.ContinueOnError)
	fset.SetOutput(io.Discard)
	email := fset.String("email", "", "")
	flagSet := fset.Bool("default", false, "")

	positional, err := parse(fset, []string{"work", "--email", "a@example.com", "--default"})
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if len(positional) != 1 || positional[0] != "work" {
		t.Errorf("positional = %v, want [work]", positional)
	}
	if *email != "a@example.com" {
		t.Errorf("email = %q, want it parsed even though it came after the name", *email)
	}
	if !*flagSet {
		t.Error("--default after a positional argument was ignored")
	}
}

func TestParse_FlagsBeforePositionalStillWork(t *testing.T) {
	fset := flag.NewFlagSet("t", flag.ContinueOnError)
	fset.SetOutput(io.Discard)
	email := fset.String("email", "", "")

	positional, err := parse(fset, []string{"--email", "a@example.com", "work"})
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if len(positional) != 1 || positional[0] != "work" {
		t.Errorf("positional = %v, want [work]", positional)
	}
	if *email != "a@example.com" {
		t.Errorf("email = %q", *email)
	}
}

func TestAdd_WritesAProfileWithoutHandEditing(t *testing.T) {
	home := fakeHome(t)

	code, out, errOut := exec2(t, "add", "personal",
		"--email", "a@example.com", "--name", "A Person", "--default")
	if code != exitOK {
		t.Fatalf("add exit = %d: %s", code, errOut)
	}
	if !strings.Contains(out, "github-personal") {
		t.Errorf("add did not report the inferred alias:\n%s", out)
	}

	code, out, errOut = exec2(t, "add", "work",
		"--email", "b@example.org", "--name", "A Person", "--dir", "~/work/")
	if code != exitOK {
		t.Fatalf("second add exit = %d: %s", code, errOut)
	}

	data := readFile(t, filepath.Join(home, ".config", "ghprofile", "profiles.yaml"))
	for _, want := range []string{"name: personal", "name: work", "alias: github-work", "~/.ssh/id_ed25519_work", "default: true"} {
		if !strings.Contains(data, want) {
			t.Errorf("profiles file missing %q:\n%s", want, data)
		}
	}
	// Paths must be written in ~ form, not expanded.
	if strings.Contains(data, home) {
		t.Errorf("an absolute path leaked into the file:\n%s", data)
	}

	if code, _, _ := exec2(t, "add", "work", "--email", "c@example.com"); code != exitError {
		t.Error("adding a duplicate name should fail")
	}
}

func TestAdd_FirstProfileBecomesTheDefault(t *testing.T) {
	home := fakeHome(t)

	if code, _, errOut := exec2(t, "add", "solo", "--email", "a@example.com", "--name", "A", "--dir", "~/x/"); code != exitOK {
		t.Fatalf("add exit = %d: %s", code, errOut)
	}

	data := readFile(t, filepath.Join(home, ".config", "ghprofile", "profiles.yaml"))
	if !strings.Contains(data, "default: true") {
		t.Errorf("the only profile was not made the default:\n%s", data)
	}
}

func TestAdd_ClaimHostNeedsConfirmation(t *testing.T) {
	home := fakeHome(t)

	// stdin is "\n" in exec2, which is not a yes.
	if code, out, errOut := exec2(t, "add", "personal", "--email", "a@example.com", "--name", "A",
		"--default", "--claim-host"); code != exitOK {
		t.Fatalf("exit = %d: %s\n%s", code, errOut, out)
	}

	data := readFile(t, filepath.Join(home, ".config", "ghprofile", "profiles.yaml"))
	if strings.Contains(data, "claim_host") {
		t.Errorf("declining the prompt still set claim_host:\n%s", data)
	}
}

func TestAdd_ClaimHostWithYes(t *testing.T) {
	home := fakeHome(t)

	if code, _, errOut := exec2(t, "add", "personal", "--email", "a@example.com", "--name", "A",
		"--default", "--claim-host", "--yes"); code != exitOK {
		t.Fatalf("exit = %d: %s", code, errOut)
	}

	data := readFile(t, filepath.Join(home, ".config", "ghprofile", "profiles.yaml"))
	if !strings.Contains(data, "claim_host: true") {
		t.Errorf("-yes did not opt in:\n%s", data)
	}
}

func TestAdd_ClaimHostWarnsBeforeAsking(t *testing.T) {
	fakeHome(t)

	_, out, _ := exec2(t, "add", "personal", "--email", "a@example.com", "--name", "A",
		"--default", "--claim-host")

	for _, want := range []string{"IdentitiesOnly yes", "stop authenticating", "fix-remote"} {
		if !strings.Contains(out, want) {
			t.Errorf("the warning does not mention %q:\n%s", want, out)
		}
	}
}

func TestAdd_ClaimHostIgnoredOnANonDefaultProfile(t *testing.T) {
	home := fakeHome(t)

	if code, _, _ := exec2(t, "add", "personal", "--email", "a@example.com", "--name", "A", "--default"); code != exitOK {
		t.Fatal("setup add failed")
	}
	if code, _, errOut := exec2(t, "add", "work", "--email", "b@example.com", "--name", "B",
		"--dir", "~/work/", "--claim-host", "--yes"); code != exitOK {
		t.Fatalf("exit = %d: %s", code, errOut)
	}

	data := readFile(t, filepath.Join(home, ".config", "ghprofile", "profiles.yaml"))
	if strings.Contains(data, "claim_host") {
		t.Errorf("a non-default profile was allowed to claim the hostname:\n%s", data)
	}
}

func TestDefault_SwitchesAndReportsClaimState(t *testing.T) {
	fakeHome(t)

	for _, args := range [][]string{
		{"add", "personal", "--email", "a@example.com", "--name", "A", "--default"},
		{"add", "work", "--email", "b@example.com", "--name", "B", "--dir", "~/work/"},
	} {
		if code, _, errOut := exec2(t, args...); code != exitOK {
			t.Fatalf("setup %v failed: %s", args, errOut)
		}
	}

	code, out, _ := exec2(t, "default")
	if code != exitOK {
		t.Fatalf("default exit = %d", code)
	}
	if !strings.Contains(out, "personal") || !strings.Contains(out, "ssh agent offers") {
		t.Errorf("default did not report the unclaimed state:\n%s", out)
	}

	if code, out, _ = exec2(t, "default", "work"); code != exitOK {
		t.Fatalf("switching exit = %d: %s", code, out)
	}
	if !strings.Contains(out, `from "personal" to "work"`) {
		t.Errorf("switch not reported:\n%s", out)
	}

	if code, out, _ = exec2(t, "default"); !strings.Contains(out, "work") {
		t.Errorf("default did not move (exit %d):\n%s", code, out)
	}
}

// doctor is what brew, flutter and npm call this, so it keeps working even
// though the help text only mentions check.
func TestCheck_DoctorStillWorksAsAnAlias(t *testing.T) {
	home := fakeHome(t)
	writeProfiles(t, home)

	code, out, errOut := exec2(t, "doctor")
	if code != exitOK {
		t.Fatalf("doctor exit = %d: %s", code, errOut)
	}

	_, viaCheck, _ := exec2(t, "check")
	if out != viaCheck {
		t.Errorf("doctor and check disagree:\n%s\nvs\n%s", out, viaCheck)
	}
}

func TestUsage_DoesNotAdvertiseDoctor(t *testing.T) {
	_, out, _ := exec2(t, "--help")
	if strings.Contains(out, "doctor") {
		t.Error("the alias should stay undocumented")
	}
	if !strings.Contains(out, "check") {
		t.Error("check is missing from the help text")
	}
}

func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "-v", "--version"} {
		code, out, errOut := exec2(t, arg)
		if code != exitOK {
			t.Fatalf("%s exit = %d: %s", arg, code, errOut)
		}
		if !strings.Contains(out, "ghprofile") {
			t.Errorf("%s output does not name the tool:\n%s", arg, out)
		}
		if !strings.Contains(out, runtime.GOOS) {
			t.Errorf("%s output does not report the platform:\n%s", arg, out)
		}
	}
}
