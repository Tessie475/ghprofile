// Package verify checks that a host alias authenticates to the expected account.
package verify

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Tessie475/ghprofile/internal/shell"
)

// ErrNoGreeting is returned when the server said nothing recognisable.
var ErrNoGreeting = errors.New("no recognisable greeting from server")

// greeting matches the line GitHub and GitLab print on a successful auth.
var greeting = regexp.MustCompile(`(?m)\bHi,? ([^!]+)!`)

// Result is the outcome of one authentication attempt.
type Result struct {
	Alias         string
	Username      string
	Authenticated bool
	Output        string
}

// SSH runs a non-interactive ssh against the alias and reads the greeting.
// GitHub exits 1 on success, so the exit code is not the signal.
func SSH(ctx context.Context, alias string) (Result, error) {
	res, err := shell.Run(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "git@"+alias)
	out := res.Stderr + res.Stdout
	r := Result{Alias: alias, Output: out}
	if err != nil {
		return r, err
	}

	if name, ok := ParseGreeting(out); ok {
		r.Username = name
		r.Authenticated = true
	}
	return r, nil
}

// ParseGreeting extracts the account name from a successful auth message.
func ParseGreeting(s string) (string, bool) {
	m := greeting.FindStringSubmatch(s)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

// Wait retries until the alias authenticates or the budget runs out, because a
// freshly uploaded key is not always live immediately.
func Wait(ctx context.Context, alias string, budget time.Duration) (Result, error) {
	deadline := time.Now().Add(budget)
	delay := time.Second

	var last Result
	for {
		r, err := SSH(ctx, alias)
		if err != nil {
			return r, err
		}
		last = r
		if r.Authenticated || time.Now().After(deadline) {
			break
		}

		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(delay):
		}
		if delay < 8*time.Second {
			delay *= 2
		}
	}

	if !last.Authenticated {
		return last, fmt.Errorf("%s: %w", alias, ErrNoGreeting)
	}
	return last, nil
}
