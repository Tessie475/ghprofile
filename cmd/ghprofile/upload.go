package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Tessie475/ghprofile/internal/config"
	"github.com/Tessie475/ghprofile/internal/keys"
	"github.com/Tessie475/ghprofile/internal/shell"
	"github.com/Tessie475/ghprofile/internal/verify"
)

// ErrNoPublicKey is returned when a profile has no public key to upload yet.
var ErrNoPublicKey = errors.New("no public key, run ghprofile apply first")

type uploadOpts struct {
	noBrowser   bool
	noClipboard bool
	timeout     time.Duration
}

func (a *app) cmdUpload(ctx context.Context, args []string) int {
	fset := a.flags("upload")
	noBrowser := fset.Bool("no-browser", false, "do not open the provider's settings page")
	noClipboard := fset.Bool("no-clipboard", false, "print the key instead of copying it")
	timeout := fset.Duration("timeout", 90*time.Second, "how long to keep retrying after you save the key")
	positional, err := parse(fset, args)
	if err != nil {
		return exitMisuse
	}

	f, code := a.load()
	if f == nil {
		return code
	}
	if len(positional) != 1 {
		fmt.Fprintln(a.errOut, "usage: ghprofile upload <profile>")
		return exitMisuse
	}

	p, ok := f.ByName(positional[0])
	if !ok {
		return a.fail(fmt.Errorf("%q: %w", positional[0], config.ErrUnknownProfile))
	}

	opts := uploadOpts{noBrowser: *noBrowser, noClipboard: *noClipboard, timeout: *timeout}
	if err := a.uploadKey(ctx, f, p, opts); err != nil {
		return a.fail(err)
	}
	return exitOK
}

// uploadKey walks the human through putting a public key on the right account,
// then proves it landed by asking the server who it thinks you are.
func (a *app) uploadKey(ctx context.Context, f *config.Profiles, p *config.Profile, opts uploadOpts) error {
	k, err := keys.Inspect(p.Key)
	if err != nil {
		return err
	}
	if !k.HasPublic {
		return fmt.Errorf("%s: %w", p.Key, ErrNoPublicKey)
	}

	pub, err := os.ReadFile(k.PublicPath)
	if err != nil {
		return err
	}

	fmt.Fprintf(a.out, "\n%s needs its key on %s\n\n", p.Name, p.Host)
	// The identity, because the profile name alone does not say which account
	// to be signed in as, and finding out by reading the pasted key is late.
	fmt.Fprintf(a.out, "  identity     %s <%s>\n", p.User.Name, p.User.Email)
	fmt.Fprintf(a.out, "  key          %s\n", k.PublicPath)
	fmt.Fprintf(a.out, "  fingerprint  %s\n", k.Fingerprint)
	fmt.Fprintln(a.out)

	switch {
	case opts.noClipboard:
		fmt.Fprintf(a.out, "%s\n", strings.TrimSpace(string(pub)))
	case shell.Copy(ctx, string(pub)) == nil:
		fmt.Fprintln(a.out, "the public key is on your clipboard")
	default:
		fmt.Fprintf(a.out, "could not reach the clipboard, here it is:\n%s\n", strings.TrimSpace(string(pub)))
	}

	url := keyURL(p.Host)
	if opts.noBrowser {
		fmt.Fprintf(a.out, "add it at %s\n", url)
	} else {
		fmt.Fprintf(a.out, "opening %s\n", url)
		if err := shell.Open(ctx, url); err != nil {
			fmt.Fprintf(a.out, "could not open a browser, add it at %s\n", url)
		}
	}

	fmt.Fprintf(a.out, "\nsign the browser in as %s, the %q account, then paste the key, save it,\nand press Enter: ", p.User.Email, p.Name)
	_, _ = bufio.NewReader(a.in).ReadString('\n')

	fmt.Fprintf(a.out, "\nchecking %s\n", p.Alias)

	c, cancel := context.WithTimeout(ctx, opts.timeout+30*time.Second)
	defer cancel()

	res, err := verify.Wait(c, p.Alias, opts.timeout)
	if err != nil {
		return fmt.Errorf("%s did not authenticate: %s", p.Alias, firstLine(res.Output))
	}

	fmt.Fprintf(a.out, "authenticated as %s\n", res.Username)

	if other, clash := a.sameAccount(ctx, f, p, res.Username); clash {
		return fmt.Errorf("%q also authenticates as %s, so one of those keys is not being used", other, res.Username)
	}
	return nil
}

// sameAccount reports another profile on the same host that answers as the
// same account, which means its key is not actually in use.
func (a *app) sameAccount(ctx context.Context, f *config.Profiles, self *config.Profile, username string) (string, bool) {
	for i := range f.Profiles {
		other := &f.Profiles[i]
		if other.Name == self.Name || !strings.EqualFold(other.Host, self.Host) {
			continue
		}

		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		res, err := verify.SSH(c, other.Alias)
		cancel()

		if err == nil && res.Authenticated && res.Username == username {
			return other.Name, true
		}
	}
	return "", false
}

// keyURL guesses where a provider keeps its SSH key settings.
func keyURL(host string) string {
	switch {
	case strings.EqualFold(host, "github.com"):
		return "https://github.com/settings/keys"
	case strings.Contains(strings.ToLower(host), "gitlab"):
		return "https://" + host + "/-/user_settings/ssh_keys"
	default:
		return "https://" + host
	}
}

func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return "no output"
}
