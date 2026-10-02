// Package blocks manages named regions of a text file delimited by markers,
// leaving everything outside those markers untouched.
package blocks

import (
	"errors"
	"fmt"
	"strings"
)

// Marker failures.
var (
	ErrUnterminated = errors.New("unterminated block")
	ErrDuplicate    = errors.New("duplicate block")
)

const (
	beginPrefix = "# BEGIN ghprofile:"
	endPrefix   = "# END ghprofile:"
)

type span struct{ begin, end int }

// List returns the names of every managed block, in the order they appear.
func List(content string) ([]string, error) {
	lines, _ := split(content)
	_, order, err := scan(lines)
	return order, err
}

// Find returns the body of the named block.
func Find(content, name string) (string, bool, error) {
	lines, _ := split(content)
	idx, _, err := scan(lines)
	if err != nil {
		return "", false, err
	}
	s, ok := idx[name]
	if !ok {
		return "", false, nil
	}
	return strings.Join(lines[s.begin+1:s.end], "\n"), true, nil
}

// Upsert replaces the named block's body in place, or appends the block when
// it is absent. Content outside the markers is preserved exactly.
func Upsert(content, name, body string) (string, error) {
	lines, trailing := split(content)
	idx, _, err := scan(lines)
	if err != nil {
		return "", err
	}

	block := render(name, body)

	if s, ok := idx[name]; ok {
		out := make([]string, 0, len(lines)+len(block))
		out = append(out, lines[:s.begin]...)
		out = append(out, block...)
		out = append(out, lines[s.end+1:]...)
		return join(out, trailing), nil
	}

	out := append([]string{}, lines...)
	// Always one separator line, so that Remove absorbing one is symmetric.
	if len(out) > 0 {
		out = append(out, "")
	}
	out = append(out, block...)
	return join(out, true), nil
}

// Prepend inserts the block at the top of the file when absent, and replaces
// it in place when already present. Use it for content that must be overridden
// by anything the user wrote below it.
func Prepend(content, name, body string) (string, error) {
	lines, trailing := split(content)
	idx, _, err := scan(lines)
	if err != nil {
		return "", err
	}

	block := render(name, body)

	if s, ok := idx[name]; ok {
		out := make([]string, 0, len(lines)+len(block))
		out = append(out, lines[:s.begin]...)
		out = append(out, block...)
		out = append(out, lines[s.end+1:]...)
		return join(out, trailing), nil
	}

	out := append([]string{}, block...)
	if len(lines) > 0 {
		out = append(out, "")
		out = append(out, lines...)
		return join(out, trailing), nil
	}
	return join(out, true), nil
}

// Remove deletes the named block. Removing an absent block is not an error.
func Remove(content, name string) (string, error) {
	lines, trailing := split(content)
	idx, _, err := scan(lines)
	if err != nil {
		return "", err
	}
	s, ok := idx[name]
	if !ok {
		return content, nil
	}

	begin := s.begin
	// Absorb one blank line above, so repeated edits do not accumulate gaps.
	if begin > 0 && strings.TrimSpace(lines[begin-1]) == "" {
		begin--
	}

	out := make([]string, 0, len(lines))
	out = append(out, lines[:begin]...)
	out = append(out, lines[s.end+1:]...)
	return join(out, trailing), nil
}

func render(name, body string) []string {
	out := []string{beginPrefix + name}
	body = strings.TrimSuffix(body, "\n")
	if body != "" {
		out = append(out, strings.Split(body, "\n")...)
	}
	return append(out, endPrefix+name)
}

func scan(lines []string) (map[string]span, []string, error) {
	idx := make(map[string]span)
	var order []string

	open := ""
	openAt := -1

	for i, ln := range lines {
		t := strings.TrimSpace(ln)

		if n, ok := cut(t, beginPrefix); ok {
			if open != "" {
				return nil, nil, fmt.Errorf("block %q opened at line %d: %w", open, openAt+1, ErrUnterminated)
			}
			if _, dup := idx[n]; dup {
				return nil, nil, fmt.Errorf("block %q at line %d: %w", n, i+1, ErrDuplicate)
			}
			open, openAt = n, i
			continue
		}

		if n, ok := cut(t, endPrefix); ok {
			if open == "" || n != open {
				return nil, nil, fmt.Errorf("end marker %q at line %d: %w", n, i+1, ErrUnterminated)
			}
			idx[open] = span{begin: openAt, end: i}
			order = append(order, open)
			open, openAt = "", -1
		}
	}

	if open != "" {
		return nil, nil, fmt.Errorf("block %q opened at line %d: %w", open, openAt+1, ErrUnterminated)
	}
	return idx, order, nil
}

func cut(line, prefix string) (string, bool) {
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
}

func split(s string) (lines []string, trailingNewline bool) {
	if s == "" {
		return nil, false
	}
	trailingNewline = strings.HasSuffix(s, "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n"), trailingNewline
}

func join(lines []string, trailingNewline bool) string {
	s := strings.Join(lines, "\n")
	if trailingNewline && s != "" {
		s += "\n"
	}
	return s
}
