package verify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tessie475/ghprofile/internal/shell"
)

const githubSuccess = "Hi Tessie475! You've successfully authenticated, but GitHub does not provide shell access.\n"

// fake records what was asked of ssh and replays canned responses, so the
// exit-code handling can be proved without a network or a key.
type fake struct {
	responses []response
	calls     int
	args      [][]string
	slept     []time.Duration
	clock     time.Time
	cancel    func()
}

type response struct {
	res shell.Result
	err error
}

func newFake(responses ...response) *fake {
	return &fake{responses: responses, clock: time.Unix(1700000000, 0).UTC()}
}

func (f *fake) checker() *Checker {
	return &Checker{
		run: func(_ context.Context, _ string, args ...string) (shell.Result, error) {
			f.args = append(f.args, args)
			i := f.calls
			f.calls++
			if i >= len(f.responses) {
				i = len(f.responses) - 1
			}
			return f.responses[i].res, f.responses[i].err
		},
		now: func() time.Time { return f.clock },
		// Advancing the clock instead of sleeping keeps the retry tests
		// instant while still exercising the real budget arithmetic.
		sleep: func(ctx context.Context, d time.Duration) error {
			f.slept = append(f.slept, d)
			f.clock = f.clock.Add(d)
			if f.cancel != nil {
				f.cancel()
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return nil
		},
	}
}

// The whole point of this package. GitHub exits 1 on a SUCCESSFUL ssh -T, so
// treating the exit code as the signal would report every working setup as
// broken. Nothing proved this before.
func TestSSH_ExitCodeOneWithAGreetingIsSuccess(t *testing.T) {
	f := newFake(response{res: shell.Result{Stderr: githubSuccess, Code: 1}})

	got, err := f.checker().SSH(context.Background(), "github-personal")
	if err != nil {
		t.Fatalf("SSH() error = %v", err)
	}
	if !got.Authenticated {
		t.Error("Authenticated = false for exit 1 with a greeting, which is what success looks like")
	}
	if got.Username != "Tessie475" {
		t.Errorf("Username = %q, want %q", got.Username, "Tessie475")
	}
	if got.Alias != "github-personal" {
		t.Errorf("Alias = %q", got.Alias)
	}
}

func TestSSH_ExitCodeOneWithoutAGreetingIsFailure(t *testing.T) {
	f := newFake(response{res: shell.Result{
		Stderr: "git@github.com: Permission denied (publickey).\n",
		Code:   1,
	}})

	got, err := f.checker().SSH(context.Background(), "github-work")
	if err != nil {
		t.Fatalf("SSH() error = %v", err)
	}
	if got.Authenticated {
		t.Error("Authenticated = true for a permission denial")
	}
	if got.Username != "" {
		t.Errorf("Username = %q, want empty", got.Username)
	}
	if !strings.Contains(got.Output, "Permission denied") {
		t.Errorf("Output lost the server's reason: %q", got.Output)
	}
}

func TestSSH_Outcomes(t *testing.T) {
	tests := []struct {
		name     string
		res      shell.Result
		wantAuth bool
		wantUser string
	}{
		{
			name:     "github, greeting on stderr, exit 1",
			res:      shell.Result{Stderr: githubSuccess, Code: 1},
			wantAuth: true,
			wantUser: "Tessie475",
		},
		{
			name:     "gitlab, greeting on stdout, exit 0",
			res:      shell.Result{Stdout: "Welcome to GitLab, @someone!\nHi someone! Welcome.\n", Code: 0},
			wantAuth: true,
			wantUser: "someone",
		},
		{
			name:     "host unreachable, exit 255",
			res:      shell.Result{Stderr: "ssh: connect to host github.com port 22: Operation timed out\n", Code: 255},
			wantAuth: false,
		},
		{
			name:     "batch mode refuses to prompt",
			res:      shell.Result{Stderr: "Host key verification failed.\n", Code: 255},
			wantAuth: false,
		},
		{
			name:     "no output at all",
			res:      shell.Result{Code: 1},
			wantAuth: false,
		},
		{
			name:     "an org deploy key with punctuation in the name",
			res:      shell.Result{Stderr: "Hi some-org/deploy-key! You've successfully authenticated.\n", Code: 1},
			wantAuth: true,
			wantUser: "some-org/deploy-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(response{res: tt.res})

			got, err := f.checker().SSH(context.Background(), "alias")
			if err != nil {
				t.Fatalf("SSH() error = %v", err)
			}
			if got.Authenticated != tt.wantAuth {
				t.Errorf("Authenticated = %v, want %v", got.Authenticated, tt.wantAuth)
			}
			if got.Username != tt.wantUser {
				t.Errorf("Username = %q, want %q", got.Username, tt.wantUser)
			}
		})
	}
}

// BatchMode is what stops ssh blocking forever on a passphrase prompt inside
// apply, so losing it would hang the tool rather than fail it.
func TestSSH_InvokesSSHCorrectly(t *testing.T) {
	f := newFake(response{res: shell.Result{Code: 1}})

	if _, err := f.checker().SSH(context.Background(), "github-work"); err != nil {
		t.Fatalf("SSH() error = %v", err)
	}
	if len(f.args) != 1 {
		t.Fatalf("ssh was called %d times, want 1", len(f.args))
	}

	joined := strings.Join(f.args[0], " ")
	for _, want := range []string{"-T", "BatchMode=yes", "StrictHostKeyChecking=accept-new", "git@github-work"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ssh args missing %q: %v", want, f.args[0])
		}
	}
}

func TestSSH_RunnerErrorIsPropagated(t *testing.T) {
	boom := errors.New("ssh not installed")
	f := newFake(response{err: boom})

	_, err := f.checker().SSH(context.Background(), "alias")
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestWait_SucceedsFirstTime(t *testing.T) {
	f := newFake(response{res: shell.Result{Stderr: githubSuccess, Code: 1}})

	got, err := f.checker().Wait(context.Background(), "alias", 90*time.Second)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if got.Username != "Tessie475" {
		t.Errorf("Username = %q", got.Username)
	}
	if f.calls != 1 {
		t.Errorf("called ssh %d times, want 1", f.calls)
	}
	if len(f.slept) != 0 {
		t.Errorf("slept %v, want not at all on first success", f.slept)
	}
}

// The case this retry loop exists for: a key that GitHub has not finished
// propagating yet.
func TestWait_RetriesUntilTheKeyGoesLive(t *testing.T) {
	denied := response{res: shell.Result{Stderr: "Permission denied (publickey).\n", Code: 1}}
	ok := response{res: shell.Result{Stderr: githubSuccess, Code: 1}}

	f := newFake(denied, denied, ok)

	got, err := f.checker().Wait(context.Background(), "alias", 90*time.Second)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if !got.Authenticated {
		t.Error("Authenticated = false after a successful retry")
	}
	if f.calls != 3 {
		t.Errorf("called ssh %d times, want 3", f.calls)
	}
	if len(f.slept) != 2 {
		t.Errorf("slept %d times, want 2", len(f.slept))
	}
}

func TestWait_BackoffDoublesAndCaps(t *testing.T) {
	denied := response{res: shell.Result{Stderr: "Permission denied (publickey).\n", Code: 1}}
	f := newFake(denied)

	// A long budget so the cap is reached before the deadline ends the loop.
	if _, err := f.checker().Wait(context.Background(), "alias", 10*time.Minute); !errors.Is(err, ErrNoGreeting) {
		t.Fatalf("error = %v, want ErrNoGreeting", err)
	}

	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	if len(f.slept) < len(want) {
		t.Fatalf("slept %v, want at least %v", f.slept, want)
	}
	for i, d := range want {
		if f.slept[i] != d {
			t.Errorf("sleep %d = %v, want %v", i, f.slept[i], d)
		}
	}
	for i, d := range f.slept {
		if d > 8*time.Second {
			t.Errorf("sleep %d = %v, which exceeds the 8s cap", i, d)
		}
	}
}

func TestWait_GivesUpWhenTheBudgetRunsOut(t *testing.T) {
	denied := response{res: shell.Result{Stderr: "Permission denied (publickey).\n", Code: 1}}
	f := newFake(denied)

	got, err := f.checker().Wait(context.Background(), "github-new", 3*time.Second)
	if !errors.Is(err, ErrNoGreeting) {
		t.Fatalf("error = %v, want it to wrap ErrNoGreeting", err)
	}
	if !strings.Contains(err.Error(), "github-new") {
		t.Errorf("error %q does not name the alias", err.Error())
	}
	if got.Authenticated {
		t.Error("Authenticated = true despite giving up")
	}
	// The last response is still returned, so the caller can show the reason.
	if !strings.Contains(got.Output, "Permission denied") {
		t.Errorf("Output = %q, want the server's reason preserved", got.Output)
	}
	if f.calls > 5 {
		t.Errorf("called ssh %d times for a 3s budget, which is not bounded by the deadline", f.calls)
	}
}

func TestWait_StopsWhenCancelled(t *testing.T) {
	denied := response{res: shell.Result{Stderr: "Permission denied (publickey).\n", Code: 1}}
	f := newFake(denied)

	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel

	_, err := f.checker().Wait(ctx, "alias", 10*time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if f.calls > 2 {
		t.Errorf("called ssh %d times after cancellation, want it to stop promptly", f.calls)
	}
}

func TestWait_RunnerErrorStopsImmediately(t *testing.T) {
	boom := errors.New("ssh not installed")
	f := newFake(response{err: boom})

	_, err := f.checker().Wait(context.Background(), "alias", 90*time.Second)
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
	if f.calls != 1 {
		t.Errorf("called ssh %d times, want 1: a missing ssh will not fix itself", f.calls)
	}
	if len(f.slept) != 0 {
		t.Errorf("slept %v, want not at all", f.slept)
	}
}

// The package-level helpers must be wired to the real ssh, not left pointing
// at a nil runner.
func TestNew_IsFullyWired(t *testing.T) {
	c := New()
	if c.run == nil || c.now == nil || c.sleep == nil {
		t.Fatalf("New() left a field nil: run=%v now=%v sleep=%v", c.run == nil, c.now == nil, c.sleep == nil)
	}
}

func TestWaitFor_ReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := waitFor(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("waitFor() = %v, want context.Canceled rather than an hour's wait", err)
	}
}

func TestWaitFor_ReturnsAfterTheDelay(t *testing.T) {
	if err := waitFor(context.Background(), time.Millisecond); err != nil {
		t.Errorf("waitFor() = %v, want nil", err)
	}
}
