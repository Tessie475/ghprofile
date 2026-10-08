// Package gitconfig renders the git identity includes for each profile.
package gitconfig

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/Tessie475/ghprofile/internal/blocks"
	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/paths"
)

// The two managed regions ghprofile owns in ~/.gitconfig.
//
// Git applies the last matching setting, so placement decides who wins. The
// unconditional default goes at the top, where anything the user wrote below
// can override it. The conditional includes go at the bottom, where they
// override everything above, including includes the user wrote by hand.
const (
	DefaultBlockName = "default-identity"
	BlockName        = "identities"
)

// Identity returns the contents of a per-profile git identity file.
func Identity(p config.Profile) string {
	var b strings.Builder
	b.WriteString("[user]\n")
	fmt.Fprintf(&b, "\tname = %s\n", p.User.Name)
	fmt.Fprintf(&b, "\temail = %s\n", p.User.Email)
	return b.String()
}

// RenderDefault returns the unconditional include for the default profile.
func RenderDefault(f *config.Profiles, pa paths.Paths) string {
	d, ok := f.DefaultProfile()
	if !ok {
		return ""
	}

	var b strings.Builder
	b.WriteString("[include]\n")
	fmt.Fprintf(&b, "\tpath = %s\n", pa.IdentityFile(d.Name))
	return b.String()
}

// RenderIncludes returns one conditional include per profile directory.
func RenderIncludes(f *config.Profiles, pa paths.Paths) string {
	var b strings.Builder
	for _, p := range f.Profiles {
		for _, dir := range p.Dirs {
			fmt.Fprintf(&b, "[includeIf \"gitdir:%s\"]\n", gitPath(dir))
			fmt.Fprintf(&b, "\tpath = %s\n", gitPath(pa.IdentityFile(p.Name)))
		}
	}
	return b.String()
}

// Apply writes both managed blocks, leaving every other section untouched.
func Apply(content string, f *config.Profiles, pa paths.Paths) (string, error) {
	out, err := place(content, DefaultBlockName, RenderDefault(f, pa), blocks.Prepend)
	if err != nil {
		return "", err
	}
	return place(out, BlockName, RenderIncludes(f, pa), blocks.Upsert)
}

// place writes a block, or removes it when there is nothing to say.
func place(content, name, body string, write func(string, string, string) (string, error)) (string, error) {
	if strings.TrimSpace(body) == "" {
		return blocks.Remove(content, name)
	}
	return write(content, name, body)
}

// HandWrittenEmail returns the email set by a [user] section the user wrote
// themselves, outside ghprofile's managed blocks.
//
// It matters because such a section sits below the default-identity block and
// therefore overrides it. Placing the block lower instead would break the
// conditional includes, so the conflict is reported rather than fought.
func HandWrittenEmail(content string) (string, bool) {
	managed := false
	inUser := false

	for _, ln := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(ln)

		switch {
		case strings.HasPrefix(trimmed, "# BEGIN ghprofile:"):
			managed = true
			continue
		case strings.HasPrefix(trimmed, "# END ghprofile:"):
			managed = false
			continue
		}
		if managed {
			continue
		}

		if strings.HasPrefix(trimmed, "[") {
			inUser = strings.HasPrefix(strings.ToLower(trimmed), "[user]")
			continue
		}
		if !inUser {
			continue
		}
		if key, value, ok := strings.Cut(trimmed, "="); ok && strings.EqualFold(strings.TrimSpace(key), "email") {
			if v := strings.TrimSpace(value); v != "" {
				return v, true
			}
		}
	}
	return "", false
}

// gitPath renders a filesystem path the way a git config file expects it.
//
// Git treats a backslash as an escape character inside a value, so a Windows
// path written raw makes \U and \G invalid escapes and git rejects the whole
// file with "bad config line". Git's own writer escapes them, which is why an
// existing config shows C:\\Users\\you. Forward slashes avoid the question
// entirely and git accepts them on every platform, including in gitdir:
// patterns.
func gitPath(p string) string {
	return toGitPath(p, runtime.GOOS == "windows")
}

// toGitPath takes the platform as an argument so the conversion is testable
// from any machine. Converting unconditionally would corrupt a Unix path that
// legitimately contains a backslash.
func toGitPath(p string, windows bool) string {
	if !windows {
		return p
	}
	return strings.ReplaceAll(p, `\`, "/")
}
