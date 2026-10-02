package sshconfig

import "strings"

// Stanza is one Host block from an ssh config file.
type Stanza struct {
	Hosts        []string
	HostName     string
	IdentityFile string
	User         string
	Options      map[string]string
	Managed      bool
}

// Stanzas parses the Host blocks in an ssh config. Directives ghprofile always
// writes land in their own fields; anything else goes in Options, which is what
// lets a hand-written stanza be adopted without losing its settings.
func Stanzas(content string) []Stanza {
	var out []Stanza
	var cur *Stanza
	managed := false

	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}

	for _, ln := range strings.Split(content, "\n") {
		t := strings.TrimSpace(ln)

		switch {
		case strings.HasPrefix(t, "# BEGIN ghprofile:"):
			flush()
			managed = true
			continue
		case strings.HasPrefix(t, "# END ghprofile:"):
			flush()
			managed = false
			continue
		}

		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}

		key, value := directive(t)
		if key == "" {
			continue
		}

		if strings.EqualFold(key, "host") {
			flush()
			cur = &Stanza{Hosts: strings.Fields(value), Options: map[string]string{}, Managed: managed}
			continue
		}
		if cur == nil {
			continue
		}

		switch strings.ToLower(key) {
		case "hostname":
			cur.HostName = value
		case "identityfile":
			cur.IdentityFile = value
		case "user":
			cur.User = value
		case "identitiesonly", "addkeystoagent", "usekeychain":
			// Always written by Render, so never inherited.
		default:
			cur.Options[key] = value
		}
	}

	flush()
	return out
}

// directive splits an ssh config line, which separates key from value with
// whitespace or, less commonly, an equals sign.
func directive(line string) (key, value string) {
	if k, v, ok := strings.Cut(line, "="); ok && !strings.ContainsAny(strings.TrimSpace(k), " \t") {
		return strings.TrimSpace(k), strings.TrimSpace(v)
	}

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", ""
	}
	return fields[0], strings.Join(fields[1:], " ")
}
