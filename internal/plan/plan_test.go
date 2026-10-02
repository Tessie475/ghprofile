package plan

import (
	"strings"
	"testing"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/gitconfig"
	"github.com/Tessie475/ghprofile/internal/keys"
	"github.com/Tessie475/ghprofile/internal/paths"
	"github.com/Tessie475/ghprofile/internal/sshconfig"
)

func testProfiles() *config.Profiles {
	return &config.Profiles{
		Version: config.Version,
		Profiles: []config.Profile{
			{Name: "personal", Host: "github.com", Alias: "github-personal", Key: "/home/u/.ssh/personal", Default: true,
				User: config.User{Name: "A", Email: "a@example.com"}},
			{Name: "work", Host: "github.com", Alias: "github-work", Key: "/home/u/.ssh/work", Dirs: []string{"/home/u/work/"},
				User: config.User{Name: "B", Email: "b@example.org"}},
		},
	}
}

// settled returns the state a converged machine would be in.
func settled(t *testing.T, f *config.Profiles, pa paths.Paths) State {
	t.Helper()

	ssh, err := sshconfig.Apply("", f, true)
	if err != nil {
		t.Fatalf("setup ssh: %v", err)
	}
	git, err := gitconfig.Apply("", f, pa)
	if err != nil {
		t.Fatalf("setup git: %v", err)
	}

	st := State{
		SSHConfig:  ssh,
		GitConfig:  git,
		Identities: map[string]string{},
		Keys:       map[string]keys.Key{},
		Dirs:       map[string]bool{pa.SSHDir: true, pa.ConfigDir: true},
	}
	for _, p := range f.Profiles {
		st.Identities[p.Name] = gitconfig.Identity(p)
		st.Keys[p.Key] = keys.Key{Path: p.Key, HasPrivate: true, HasPublic: true, Mode: keys.PrivateMode}
	}
	return st
}

func TestCompute_ConvergedMachineNeedsNothing(t *testing.T) {
	f := testProfiles()
	pa := paths.Default("/home/u")

	actions, err := Compute(f, settled(t, f, pa), pa, true)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(actions) != 0 {
		for _, a := range actions {
			t.Logf("unexpected: %s (%s)", a, a.Reason)
		}
		t.Fatalf("Compute() returned %d actions, want 0", len(actions))
	}
}

func TestCompute_EmptyMachine(t *testing.T) {
	f := testProfiles()
	pa := paths.Default("/home/u")

	st := State{Identities: map[string]string{}, Keys: map[string]keys.Key{}, Dirs: map[string]bool{}}

	actions, err := Compute(f, st, pa, true)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}

	counts := map[Kind]int{}
	for _, a := range actions {
		counts[a.Kind]++
	}
	if counts[MakeDir] != 2 {
		t.Errorf("MakeDir = %d, want 2 (.ssh and the config dir)", counts[MakeDir])
	}
	if counts[MakeKey] != 2 {
		t.Errorf("MakeKey = %d, want 2, one per profile", counts[MakeKey])
	}
	// Two identity files plus the ssh config plus the git config.
	if counts[WriteFile] != 4 {
		t.Errorf("WriteFile = %d, want 4", counts[WriteFile])
	}
}

func TestCompute_FixesLoosePermissions(t *testing.T) {
	f := testProfiles()
	pa := paths.Default("/home/u")

	st := settled(t, f, pa)
	st.Keys["/home/u/.ssh/work"] = keys.Key{Path: "/home/u/.ssh/work", HasPrivate: true, HasPublic: true, Mode: 0o644}

	actions, err := Compute(f, st, pa, true)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(actions) != 1 || actions[0].Kind != Chmod {
		t.Fatalf("actions = %v, want a single Chmod", actions)
	}
	if actions[0].Mode != keys.PrivateMode {
		t.Errorf("Mode = %v, want %v", actions[0].Mode, keys.PrivateMode)
	}
}

func TestCompute_DoesNotTouchTheFilesystem(t *testing.T) {
	f := testProfiles()
	pa := paths.Default("/nonexistent/home/that/should/never/be/read")

	st := State{Identities: map[string]string{}, Keys: map[string]keys.Key{}, Dirs: map[string]bool{}}
	if _, err := Compute(f, st, pa, true); err != nil {
		t.Fatalf("Compute() error = %v, want nil: it must not depend on the filesystem", err)
	}
}

func TestAction_Diff(t *testing.T) {
	a := Action{
		Kind:    WriteFile,
		Before:  "same\nold line\nalso same\n",
		Content: "same\nnew line\nalso same\n",
	}

	got := a.Diff()
	if !strings.Contains(got, "- old line") {
		t.Errorf("diff missing the removal:\n%s", got)
	}
	if !strings.Contains(got, "+ new line") {
		t.Errorf("diff missing the addition:\n%s", got)
	}
	if strings.Contains(got, "same") {
		t.Errorf("diff should trim unchanged context:\n%s", got)
	}
}
