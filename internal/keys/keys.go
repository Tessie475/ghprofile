// Package keys inspects and generates the SSH keys backing each profile.
package keys

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/Tessie475/ghprofile/internal/shell"
)

// Key problems.
var (
	ErrMalformedPublicKey = errors.New("malformed public key")
	ErrKeygen             = errors.New("ssh-keygen failed")
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

// PermissionsOK reports whether the private key is tight enough for OpenSSH.
func (k Key) PermissionsOK() bool {
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

// Generate creates an ed25519 keypair at path with no passphrase.
func Generate(ctx context.Context, path, comment string) error {
	res, err := shell.Run(ctx, "ssh-keygen", "-t", "ed25519", "-f", path, "-C", comment, "-N", "")
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("%w: %s", ErrKeygen, strings.TrimSpace(res.Stderr+res.Stdout))
	}
	return nil
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
	return strings.HasPrefix(fields[0], "ssh-") || strings.HasPrefix(fields[0], "ecdsa-")
}
