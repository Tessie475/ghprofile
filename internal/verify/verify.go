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

// Checker performs the authentication checks.
//
// The ssh invocation, the clock and the sleep are fields rather than direct
// calls, so that the exit-code handling and the retry loop can be tested
// without a network, a key, or real time passing. This is the package that
// tells a user whether their setup works, so being able to prove it behaves is
// worth the indirection.
type Checker struct {
	run   func(context.Context, string, ...string) (shell.Result, error)
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// New returns a Checker that talks to the real ssh.
func New() *Checker {
	return &Checker{run: shell.Run, now: time.Now, sleep: waitFor}
}

// SSH runs a non-interactive ssh against the alias and reads the greeting.
//
// GitHub exits 1 on a successful "ssh -T", so the exit code is not the signal.
// The greeting is.
func (c *Checker) SSH(ctx context.Context, alias string) (Result, error) {
	res, err := c.run(ctx, "ssh",
		"-T",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"git@"+alias,
	)

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

// Wait retries until the alias authenticates or the budget runs out, because a
// freshly uploaded key is not always live immediately.
func (c *Checker) Wait(ctx context.Context, alias string, budget time.Duration) (Result, error) {
	deadline := c.now().Add(budget)
	delay := time.Second

	var last Result
	for {
		r, err := c.SSH(ctx, alias)
		if err != nil {
			return r, err
		}
		last = r

		if r.Authenticated || c.now().After(deadline) {
			break
		}
		if err := c.sleep(ctx, delay); err != nil {
			return last, err
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

// ParseGreeting extracts the account name from a successful auth message.
func ParseGreeting(s string) (string, bool) {
	m := greeting.FindStringSubmatch(s)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

// SSH checks one alias using the real ssh.
func SSH(ctx context.Context, alias string) (Result, error) {
	return New().SSH(ctx, alias)
}

// Wait retries one alias using the real ssh until it authenticates.
func Wait(ctx context.Context, alias string, budget time.Duration) (Result, error) {
	return New().Wait(ctx, alias, budget)
}

// waitFor sleeps, but gives up early if the caller is cancelled.
func waitFor(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
