package sshconfig

import "testing"

func TestStanzas(t *testing.T) {
	const content = `# a comment
Host github-work gh-work
  HostName github.com
  User git
  IdentityFile ~/.ssh/id_work
  IdentitiesOnly yes
  ServerAliveInterval 60
  ProxyJump bastion

Host bastion
  HostName bastion.example.com

# BEGIN ghprofile:personal
Host github-personal
  HostName github.com
  IdentityFile ~/.ssh/personal
# END ghprofile:personal
`

	got := Stanzas(content)
	if len(got) != 3 {
		t.Fatalf("got %d stanzas, want 3", len(got))
	}

	work := got[0]
	if len(work.Hosts) != 2 || work.Hosts[0] != "github-work" || work.Hosts[1] != "gh-work" {
		t.Errorf("Hosts = %v, want both patterns", work.Hosts)
	}
	if work.HostName != "github.com" {
		t.Errorf("HostName = %q", work.HostName)
	}
	if work.IdentityFile != "~/.ssh/id_work" {
		t.Errorf("IdentityFile = %q", work.IdentityFile)
	}
	if work.User != "git" {
		t.Errorf("User = %q", work.User)
	}
	if work.Managed {
		t.Error("a hand-written stanza was reported as managed")
	}

	// IdentitiesOnly is always written by Render, so it must not be inherited.
	if _, found := work.Options["IdentitiesOnly"]; found {
		t.Error("IdentitiesOnly leaked into Options")
	}
	if work.Options["ServerAliveInterval"] != "60" {
		t.Errorf("Options[ServerAliveInterval] = %q, want 60", work.Options["ServerAliveInterval"])
	}
	if work.Options["ProxyJump"] != "bastion" {
		t.Errorf("Options[ProxyJump] = %q, want bastion", work.Options["ProxyJump"])
	}

	if !got[2].Managed {
		t.Error("a stanza inside a managed block was not flagged as managed")
	}
}

func TestStanzas_EqualsSeparator(t *testing.T) {
	got := Stanzas("Host=github-work\n  HostName=github.com\n  ServerAliveInterval=60\n")
	if len(got) != 1 {
		t.Fatalf("got %d stanzas, want 1", len(got))
	}
	if got[0].HostName != "github.com" {
		t.Errorf("HostName = %q", got[0].HostName)
	}
	if got[0].Options["ServerAliveInterval"] != "60" {
		t.Errorf("Options = %v", got[0].Options)
	}
}

func TestStanzas_Empty(t *testing.T) {
	if got := Stanzas(""); len(got) != 0 {
		t.Errorf("Stanzas(empty) = %v, want none", got)
	}
}

// Everything a hand-written stanza carried must come back out, or adopting it
// loses settings.
func TestStanzas_RoundTripsThroughRender(t *testing.T) {
	const content = "Host github-work\n  HostName github.com\n  IdentityFile ~/.ssh/k\n  ServerAliveInterval 60\n"

	s := Stanzas(content)[0]
	p := testProfiles().Profiles[1]
	p.Options = s.Options

	if out := Render(p, true, false); !containsAll(out, "ServerAliveInterval 60") {
		t.Errorf("inherited option lost on render:\n%s", out)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
