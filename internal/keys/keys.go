// Package keys inspects and generates the SSH keys backing each profile.
package keys

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Tessie475/ghprofile/internal/shell"
)

// Key problems.
var (
	ErrMalformedPublicKey   = errors.New("malformed public key")
	ErrKeygen               = errors.New("ssh-keygen failed")
	ErrMalformedPrivateKey  = errors.New("malformed private key")
	ErrUnsupportedKeyFormat = errors.New("unsupported private key format")
)

// Permissions OpenSSH insists on.
const (
	DirMode     fs.FileMode = 0o700
	PrivateMode fs.FileMode = 0o600
	PublicMode  fs.FileMode = 0o644
)

// Key describes a keypair on disk, whether or not both halves are present.
type Key struct {
	Path        string
	PublicPath  string
	HasPrivate  bool
	HasPublic   bool
	Mode        fs.FileMode
	Type        string
	Comment     string
	Fingerprint string
}

// Orphaned reports a public key with no matching private key.
func (k Key) Orphaned() bool { return k.HasPublic && !k.HasPrivate }

// Complete reports that both halves are present.
func (k Key) Complete() bool { return k.HasPrivate && k.HasPublic }

// unixPermissions is false on Windows, where the mode bits say nothing about
// who can read a key. Go reports every writable file as 0666 and os.Chmod can
// only toggle read-only, so checking them reported a key as too open on every
// run and "fixed" it with a chmod that changed nothing. Windows protects keys
// with ACLs, which ssh-keygen sets correctly. A variable rather than a direct
// runtime.GOOS check, so tests on any platform can exercise the Windows branch.
var unixPermissions = runtime.GOOS != "windows"

// PermissionsOK reports whether the private key is tight enough for OpenSSH.
func (k Key) PermissionsOK() bool {
	if !unixPermissions {
		return true
	}
	return !k.HasPrivate || k.Mode.Perm()&0o077 == 0
}

// Inspect reads whatever exists at path and at path with a .pub suffix.
func Inspect(path string) (Key, error) {
	k := Key{Path: path, PublicPath: path + ".pub"}

	if info, err := os.Stat(path); err == nil {
		k.HasPrivate = true
		k.Mode = info.Mode()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return k, fmt.Errorf("stat %s: %w", path, err)
	}

	pub, err := os.ReadFile(k.PublicPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return k, nil
		}
		return k, fmt.Errorf("read %s: %w", k.PublicPath, err)
	}
	k.HasPublic = true

	fields := strings.Fields(string(pub))
	if len(fields) >= 1 {
		k.Type = fields[0]
	}
	if len(fields) >= 3 {
		k.Comment = strings.Join(fields[2:], " ")
	}
	if fp, err := Fingerprint(pub); err == nil {
		k.Fingerprint = fp
	}

	return k, nil
}

// Fingerprint returns the SHA256 fingerprint OpenSSH prints for a public key.
func Fingerprint(pub []byte) (string, error) {
	fields := strings.Fields(string(pub))
	if len(fields) < 2 {
		return "", ErrMalformedPublicKey
	}
	raw, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrMalformedPublicKey, err)
	}
	sum := sha256.Sum256(raw)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

// Generate creates an ed25519 keypair at path.
//
// By default ssh-keygen runs with the terminal attached and asks for a
// passphrase, which is exactly what GitHub's own documented command does.
// Pressing Enter twice declines and produces an unencrypted key, so the choice
// is the user's and they are told it exists.
//
// With stdin closed, as in CI, ssh-keygen reads EOF as an empty passphrase and
// carries on rather than hanging. noPassphrase makes that explicit for scripts
// that would rather say so than rely on it.
//
// The passphrase is never accepted as an argument. Anything in argv is visible
// in the process list and, if typed, in shell history, which is a poor place
// for the thing protecting a private key.
func Generate(ctx context.Context, path, comment string, noPassphrase bool) error {
	args := []string{"-t", "ed25519", "-f", path, "-C", comment}

	if !noPassphrase {
		// No -N, so ssh-keygen prompts for and confirms the passphrase.
		return shell.RunInteractive(ctx, "ssh-keygen", args...)
	}

	res, err := shell.Run(ctx, "ssh-keygen", append(args, "-N", "")...)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("%w: %s", ErrKeygen, strings.TrimSpace(res.Stderr+res.Stdout))
	}
	return nil
}

// IsEncrypted reports whether a private key is protected by a passphrase.
//
// The tool cannot know what the user typed at the prompt, and the answer
// decides whether the key has to go into the agent before verification can use
// it. So it reads the answer off the file instead of guessing.
//
// Two formats are understood. A modern OpenSSH key records its cipher in clear
// text near the start of the base64 body. A legacy PEM key, which plenty of
// people are still carrying from an RSA key made years ago, announces
// encryption with a Proc-Type header above the base64 instead.
//
// Anything else returns ErrUnsupportedKeyFormat rather than
// ErrMalformedPrivateKey, because telling someone their working key is
// malformed is the wrong answer.
func IsEncrypted(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	if !LooksPrivate(data) {
		return false, ErrMalformedPrivateKey
	}

	switch h := head(data); {
	case strings.Contains(h, "BEGIN OPENSSH PRIVATE KEY"):
		cipher, err := privateKeyCipher(data)
		if err != nil {
			return false, err
		}
		return cipher != "none", nil

	// PKCS#8 says so in the armor line itself.
	case strings.Contains(h, "BEGIN ENCRYPTED PRIVATE KEY"):
		return true, nil
	case strings.Contains(h, "BEGIN PRIVATE KEY"):
		return false, nil

	// Traditional OpenSSL PEM, as ssh-keygen -m PEM writes and as older
	// versions wrote by default. Encryption is announced in clear text above
	// the base64; an unencrypted one carries no headers at all.
	case isLegacyPEM(h):
		return strings.Contains(h, "Proc-Type:") && strings.Contains(h, "ENCRYPTED"), nil

	default:
		return false, ErrUnsupportedKeyFormat
	}
}

// legacyPEMTypes are the traditional OpenSSL armor lines whose encryption can
// be read from the headers.
var legacyPEMTypes = []string{
	"BEGIN RSA PRIVATE KEY",
	"BEGIN DSA PRIVATE KEY",
	"BEGIN EC PRIVATE KEY",
}

func isLegacyPEM(head string) bool {
	for _, armor := range legacyPEMTypes {
		if strings.Contains(head, armor) {
			return true
		}
	}
	return false
}

// head returns enough of the file to cover the armor line and any headers,
// without reading a whole key into a second string.
func head(data []byte) string {
	const n = 512
	if len(data) > n {
		data = data[:n]
	}
	return string(data)
}

const opensshMagic = "openssh-key-v1\x00"

func privateKeyCipher(pem []byte) (string, error) {
	var body strings.Builder
	for _, line := range strings.Split(string(pem), "\n") {
		if strings.HasPrefix(line, "-----") || strings.TrimSpace(line) == "" {
			continue
		}
		body.WriteString(strings.TrimSpace(line))
	}

	raw, err := base64.StdEncoding.DecodeString(body.String())
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrMalformedPrivateKey, err)
	}

	magic := []byte(opensshMagic)
	if !bytes.HasPrefix(raw, magic) {
		// Valid PEM armor, but not a format whose cipher this can read, such
		// as PKCS#8. Not the same thing as a corrupt file.
		return "", ErrUnsupportedKeyFormat
	}

	rest := raw[len(magic):]
	if len(rest) < 4 {
		return "", ErrMalformedPrivateKey
	}

	n := binary.BigEndian.Uint32(rest[:4])
	rest = rest[4:]

	// int64 on both sides, so neither conversion can wrap. Subtracting before
	// converting would underflow on a truncated file and let the check pass.
	if int64(n) > int64(len(rest)) {
		return "", ErrMalformedPrivateKey
	}
	return string(rest[:n]), nil
}

// AddToAgent loads a key into the running ssh agent.
//
// A passphrase-protected key is unusable by anything that cannot prompt, and
// verification deliberately runs ssh with BatchMode=yes so it never blocks. So
// a freshly encrypted key has to be put in the agent, or the check that
// follows would report a perfectly good setup as broken.
//
// useKeychain stores the passphrase in the macOS login keychain, which is what
// makes this a one-time cost rather than a prompt per session.
func AddToAgent(ctx context.Context, path string, useKeychain bool) error {
	args := []string{}
	if useKeychain {
		args = append(args, "--apple-use-keychain")
	}
	return shell.RunInteractive(ctx, "ssh-add", append(args, path)...)
}

// LooksPrivate reports whether data is private key material rather than a
// public key. File extensions lie: a private key saved as something.pub is a
// mistake people really make, and reporting it as a harmless public key is
// worse than not reporting it at all.
func LooksPrivate(data []byte) bool {
	head := string(data)
	if len(head) > 200 {
		head = head[:200]
	}
	return strings.Contains(head, "PRIVATE KEY-----")
}

// LooksPublic reports whether data is a single-line public key.
func LooksPublic(data []byte) bool {
	fields := strings.Fields(string(data))
	if len(fields) < 2 || LooksPrivate(data) {
		return false
	}
	return isKeyType(fields[0])
}

// publicKeyTypes are the algorithm names OpenSSH writes at the start of a
// public key. The sk- forms are FIDO hardware keys, which are exactly what
// security-conscious users carry, so missing them would be a poor joke.
var publicKeyTypes = []string{
	"ssh-",
	"ecdsa-",
	"sk-ssh-",
	"sk-ecdsa-",
}

func isKeyType(field string) bool {
	for _, prefix := range publicKeyTypes {
		if strings.HasPrefix(field, prefix) {
			return true
		}
	}
	return false
}

// Existing returns the complete keypairs already in a directory.
//
// Someone with a working key and a working account should not be told to
// generate a second one, and nothing surfaced that the choice existed.
func Existing(dir string) ([]Key, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var out []Key
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pub") {
			continue
		}

		k, err := Inspect(filepath.Join(dir, strings.TrimSuffix(e.Name(), ".pub")))
		if err != nil || !k.Complete() {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}
