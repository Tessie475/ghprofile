// Package sshconfig renders and updates the OpenSSH client configuration.
package sshconfig

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Tessie475/ghprofile/internal/blocks"
	"github.com/Tessie475/ghprofile/internal/config"
)

// Render returns the Host stanza for a profile.
//
// When claimHost is true the real hostname is added as a second pattern, so a
// bare URL such as git@github.com:owner/repo resolves to this identity instead
// of to whatever key the agent happens to offer first. Only the default
// profile should claim it, since OpenSSH matches the first stanza that fits.
func Render(p config.Profile, darwin, claimHost bool) string {
	patterns := p.Alias
	if claimHost && !strings.EqualFold(p.Alias, p.Host) {
		patterns += " " + p.Host
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Host %s\n", patterns)
	fmt.Fprintf(&b, "  HostName %s\n", p.Host)
	b.WriteString("  User git\n")
	fmt.Fprintf(&b, "  IdentityFile %s\n", p.Key)
	b.WriteString("  IdentitiesOnly yes\n")
	b.WriteString("  AddKeysToAgent yes\n")
	if darwin {
		b.WriteString("  UseKeychain yes\n")
	}

	for _, k := range sortedOptionKeys(p.Options) {
		fmt.Fprintf(&b, "  %s %s\n", k, p.Options[k])
	}
	return b.String()
}

// fixed are the directives Render always writes, so an option cannot silently
// duplicate one. OpenSSH honours the first occurrence of most keywords.
var fixed = map[string]bool{
	"hostname": true, "user": true, "identityfile": true,
	"identitiesonly": true, "addkeystoagent": true, "usekeychain": true,
}

// sortedOptionKeys gives a stable order, without which the rendered stanza
// would differ run to run and apply would never converge.
func sortedOptionKeys(opts map[string]string) []string {
	out := make([]string, 0, len(opts))
	for k := range opts {
		if !fixed[strings.ToLower(k)] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Apply writes a managed block per profile and drops managed blocks whose
// profile has gone away. Hand-written stanzas are left alone.
func Apply(content string, f *config.Profiles, darwin bool) (string, error) {
	want := make(map[string]bool, len(f.Profiles))
	for _, p := range f.Profiles {
		want[p.Name] = true
	}

	existing, err := blocks.List(content)
	if err != nil {
		return "", err
	}
	out := content
	for _, name := range existing {
		if !want[name] {
			if out, err = blocks.Remove(out, name); err != nil {
				return "", err
			}
		}
	}

	// The default profile goes first, so its claim on the bare hostname is the
	// first match OpenSSH finds for a plain URL.
	ordered := make([]config.Profile, 0, len(f.Profiles))
	if d, ok := f.DefaultProfile(); ok {
		ordered = append(ordered, *d)
	}
	for _, p := range f.Profiles {
		if !p.Default {
			ordered = append(ordered, p)
		}
	}

	for _, p := range ordered {
		if out, err = blocks.Upsert(out, p.Name, Render(p, darwin, p.Default && p.ClaimHost)); err != nil {
			return "", err
		}
	}
	return out, nil
}

// Hosts returns every Host pattern declared in the file, managed or not.
func Hosts(content string) []string { return hosts(content, false) }

// handWritten returns Host patterns declared outside any managed block.
func handWritten(content string) []string { return hosts(content, true) }

func hosts(content string, skipManaged bool) []string {
	var out []string
	managed := false

	for _, ln := range strings.Split(content, "\n") {
		t := strings.TrimSpace(ln)

		switch {
		case strings.HasPrefix(t, "# BEGIN ghprofile:"):
			managed = true
			continue
		case strings.HasPrefix(t, "# END ghprofile:"):
			managed = false
			continue
		}

		if skipManaged && managed {
			continue
		}
		if len(t) < 5 || !strings.EqualFold(t[:5], "host ") {
			continue
		}
		out = append(out, strings.Fields(t[5:])...)
	}
	return out
}

// Shadowed returns Host patterns ghprofile means to own that also appear in a
// hand-written stanza. OpenSSH uses the first matching Host, so the
// hand-written one wins and the managed block has no effect until it is
// removed.
//
// This includes the bare hostname when the default profile claims it.
func Shadowed(content string, f *config.Profiles) []string {
	alias := make(map[string]string, len(f.Profiles)+1)
	for _, p := range f.Profiles {
		alias[strings.ToLower(p.Alias)] = p.Alias
	}
	if d, ok := f.DefaultProfile(); ok && d.ClaimHost && !strings.EqualFold(d.Alias, d.Host) {
		alias[strings.ToLower(d.Host)] = d.Host
	}

	var out []string
	seen := map[string]bool{}
	for _, h := range handWritten(content) {
		k := strings.ToLower(h)
		if name, ok := alias[k]; ok && !seen[k] {
			seen[k] = true
			out = append(out, name)
		}
	}
	return out
}

// Unmanaged returns Host patterns that exist in the file but are not owned by
// any profile.
func Unmanaged(content string, f *config.Profiles) []string {
	managed := make(map[string]bool, len(f.Profiles))
	for _, p := range f.Profiles {
		managed[strings.ToLower(p.Alias)] = true
	}

	var out []string
	seen := map[string]bool{}
	for _, h := range Hosts(content) {
		k := strings.ToLower(h)
		if managed[k] || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, h)
	}
	return out
}

// RemoveHost deletes a hand-written Host stanza so that a managed block can
// take its place. Stanzas inside managed blocks are left alone.
func RemoveHost(content, alias string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))

	dropping := false
	managed := false

	for _, ln := range lines {
		t := strings.TrimSpace(ln)

		switch {
		case strings.HasPrefix(t, "# BEGIN ghprofile:"):
			managed = true
		case strings.HasPrefix(t, "# END ghprofile:"):
			managed = false
		}

		if !managed && len(t) >= 5 && strings.EqualFold(t[:5], "host ") {
			dropping = false
			for _, h := range strings.Fields(t[5:]) {
				if strings.EqualFold(h, alias) {
					dropping = true
				}
			}
		}

		if dropping {
			if t == "" {
				dropping = false
			}
			continue
		}
		out = append(out, ln)
	}

	return strings.Join(out, "\n")
}
