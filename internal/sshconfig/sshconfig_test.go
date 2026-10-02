package sshconfig

import (
	"strings"
	"testing"

	"github.com/Tessie475/ghprofile/internal/config"
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

func TestRender(t *testing.T) {
	got := Render(testProfiles().Profiles[1], true, false)

	for _, want := range []string{
		"Host github-work",
		"HostName github.com",
		"User git",
		"IdentityFile /home/u/.ssh/work",
		"IdentitiesOnly yes",
		"AddKeysToAgent yes",
		"UseKeychain yes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stanza missing %q:\n%s", want, got)
		}
	}
}

func TestRender_UseKeychainIsDarwinOnly(t *testing.T) {
	if strings.Contains(Render(testProfiles().Profiles[0], false, false), "UseKeychain") {
		t.Error("UseKeychain must not appear on non-darwin")
	}
}

func TestApply_PreservesHandWrittenStanzas(t *testing.T) {
	const existing = "Host bastion\n  HostName bastion.example.com\n  ProxyJump none\n"

	got, err := Apply(existing, testProfiles(), true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !strings.Contains(got, "Host bastion") || !strings.Contains(got, "ProxyJump none") {
		t.Error("hand-written stanza was lost")
	}
	if !strings.Contains(got, "# BEGIN ghprofile:work") {
		t.Error("managed block missing")
	}
}

func TestApply_IsIdempotent(t *testing.T) {
	once, err := Apply("", testProfiles(), true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	twice, err := Apply(once, testProfiles(), true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if once != twice {
		t.Errorf("Apply is not idempotent:\nfirst:\n%s\nsecond:\n%s", once, twice)
	}
}

func TestApply_DropsBlocksForRemovedProfiles(t *testing.T) {
	f := testProfiles()
	full, err := Apply("", f, true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	f.Profiles = f.Profiles[:1]
	trimmed, err := Apply(full, f, true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if strings.Contains(trimmed, "ghprofile:work") {
		t.Error("block for a removed profile survived")
	}
	if !strings.Contains(trimmed, "ghprofile:personal") {
		t.Error("block for a surviving profile was dropped")
	}
}

func TestHosts(t *testing.T) {
	const content = "Host a b\n  HostName x\nHOST c\n  HostName y\n"

	got := Hosts(content)
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("Hosts() = %v, want [a b c]", got)
	}
}

func TestUnmanaged(t *testing.T) {
	const content = "Host bastion\nHost github-work\n"

	got := Unmanaged(content, testProfiles())
	if len(got) != 1 || got[0] != "bastion" {
		t.Errorf("Unmanaged() = %v, want [bastion]", got)
	}
}

func TestRemoveHost(t *testing.T) {
	const content = "# Work\nHost github-work\n  HostName github.com\n\nHost bastion\n  HostName b\n"

	got := RemoveHost(content, "github-work")
	if strings.Contains(got, "Host github-work") {
		t.Errorf("stanza survived:\n%s", got)
	}
	if !strings.Contains(got, "Host bastion") {
		t.Errorf("unrelated stanza was removed:\n%s", got)
	}
}

func TestRemoveHost_LeavesManagedBlocksAlone(t *testing.T) {
	content, err := Apply("", testProfiles(), true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if got := RemoveHost(content, "github-work"); got != content {
		t.Error("RemoveHost touched a managed block")
	}
}

func TestShadowed(t *testing.T) {
	const handwritten = "Host github-work\n  HostName github.com\n"

	managed, err := Apply(handwritten, testProfiles(), true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got := Shadowed(managed, testProfiles())
	if len(got) != 1 || got[0] != "github-work" {
		t.Errorf("Shadowed() = %v, want [github-work]", got)
	}
}

func TestShadowed_NoneAfterAdopt(t *testing.T) {
	const handwritten = "Host github-work\n  HostName github.com\n"

	adopted := RemoveHost(handwritten, "github-work")
	managed, err := Apply(adopted, testProfiles(), true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if got := Shadowed(managed, testProfiles()); len(got) != 0 {
		t.Errorf("Shadowed() = %v, want none after adopt", got)
	}
}

func TestRender_Options(t *testing.T) {
	p := testProfiles().Profiles[1]
	p.Options = map[string]string{
		"ServerAliveInterval": "60",
		"ServerAliveCountMax": "30",
	}

	got := Render(p, true, false)
	for _, want := range []string{"ServerAliveInterval 60", "ServerAliveCountMax 30"} {
		if !strings.Contains(got, want) {
			t.Errorf("stanza missing %q:\n%s", want, got)
		}
	}
}

// Map iteration order is random, so an unsorted render would make apply
// rewrite the file forever.
func TestRender_OptionsAreDeterministic(t *testing.T) {
	p := testProfiles().Profiles[1]
	p.Options = map[string]string{"Alpha": "1", "Beta": "2", "Gamma": "3", "Delta": "4", "Epsilon": "5"}

	first := Render(p, true, false)
	for i := 0; i < 50; i++ {
		if got := Render(p, true, false); got != first {
			t.Fatalf("render is not stable across calls:\n%s\nvs\n%s", first, got)
		}
	}
}

func TestRender_OptionsCannotDuplicateFixedDirectives(t *testing.T) {
	p := testProfiles().Profiles[1]
	p.Options = map[string]string{"IdentitiesOnly": "no", "HostName": "evil.example.com"}

	got := Render(p, true, false)
	if strings.Count(got, "IdentitiesOnly") != 1 {
		t.Errorf("IdentitiesOnly appears more than once:\n%s", got)
	}
	if strings.Contains(got, "evil.example.com") {
		t.Errorf("an option overrode HostName:\n%s", got)
	}
}

func TestRender_ClaimsTheBareHostname(t *testing.T) {
	p := testProfiles().Profiles[0]

	withClaim := Render(p, true, true)
	if !strings.Contains(withClaim, "Host github-personal github.com") {
		t.Errorf("the default stanza does not claim the bare hostname:\n%s", withClaim)
	}

	without := Render(p, true, false)
	if strings.Contains(without, "Host github-personal github.com") {
		t.Errorf("a non-default stanza claimed the bare hostname:\n%s", without)
	}
}

// A profile whose alias is already the hostname must not list it twice.
func TestRender_DoesNotDuplicateThePattern(t *testing.T) {
	p := testProfiles().Profiles[0]
	p.Alias = p.Host

	if got := Render(p, true, true); strings.Contains(got, "Host github.com github.com") {
		t.Errorf("pattern duplicated:\n%s", got)
	}
}

func TestApply_OnlyTheDefaultClaimsTheHostname(t *testing.T) {
	f := testProfiles()
	f.Profiles[0].ClaimHost = true

	out, err := Apply("", f, true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if !strings.Contains(out, "Host github-personal github.com") {
		t.Errorf("default did not claim the hostname:\n%s", out)
	}
	if strings.Contains(out, "Host github-work github.com") {
		t.Errorf("a non-default profile claimed the hostname:\n%s", out)
	}
}

// OpenSSH uses the first matching stanza, so the claim is worthless unless the
// default's block comes before the others.
func TestApply_DefaultStanzaComesFirst(t *testing.T) {
	f := testProfiles()
	// Put the default last in the document, to prove ordering is not accidental.
	f.Profiles[0], f.Profiles[1] = f.Profiles[1], f.Profiles[0]

	out, err := Apply("", f, true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	def := strings.Index(out, "ghprofile:personal")
	other := strings.Index(out, "ghprofile:work")
	if def < 0 || other < 0 {
		t.Fatalf("a block is missing:\n%s", out)
	}
	if def > other {
		t.Errorf("the default block must come first:\n%s", out)
	}
}

func TestShadowed_IncludesTheClaimedHostname(t *testing.T) {
	const handwritten = "Host github.com\n  IdentityFile ~/.ssh/whatever\n"

	f := testProfiles()
	f.Profiles[0].ClaimHost = true

	managed, err := Apply(handwritten, f, true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got := Shadowed(managed, f)
	found := false
	for _, h := range got {
		if h == "github.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("Shadowed() = %v, want it to include github.com", got)
	}
}

// The claim is dangerous enough that it must never happen unless asked for.
func TestApply_DoesNotClaimTheHostnameByDefault(t *testing.T) {
	out, err := Apply("", testProfiles(), true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if strings.Contains(out, "Host github-personal github.com") {
		t.Errorf("the hostname was claimed without claim_host being set:\n%s", out)
	}
}

func TestShadowed_IgnoresTheHostnameWhenUnclaimed(t *testing.T) {
	const handwritten = "Host github.com\n  IdentityFile ~/.ssh/whatever\n"

	managed, err := Apply(handwritten, testProfiles(), true)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for _, h := range Shadowed(managed, testProfiles()) {
		if h == "github.com" {
			t.Error("an unclaimed hostname was reported as shadowed, so adopt would delete the user's stanza")
		}
	}
}
