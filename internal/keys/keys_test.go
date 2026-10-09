package keys

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fixturePub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEQfP22gB37vYLQ4zXLaoUSVB1smpOfN+7oBj0oWre3y fixture@example.com\n"
	fixtureFP  = "SHA256:l6bze6cXg3VizXCJ1RwUIrg7jkE44nW3+d5AryXuhjE"
)

func TestFingerprint(t *testing.T) {
	got, err := Fingerprint([]byte(fixturePub))
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	if got != fixtureFP {
		t.Errorf("Fingerprint() = %q, want %q (as printed by ssh-keygen -lf)", got, fixtureFP)
	}
}

func TestFingerprint_Errors(t *testing.T) {
	for _, in := range []string{"", "ssh-ed25519", "ssh-ed25519 not-base64!!"} {
		if _, err := Fingerprint([]byte(in)); !errors.Is(err, ErrMalformedPublicKey) {
			t.Errorf("Fingerprint(%q) error = %v, want ErrMalformedPublicKey", in, err)
		}
	}
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	priv := filepath.Join(dir, "id_ed25519")

	if err := os.WriteFile(priv, []byte("PRIVATE"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(priv+".pub", []byte(fixturePub), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	k, err := Inspect(priv)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !k.Complete() {
		t.Error("Complete() = false, want true")
	}
	if k.Orphaned() {
		t.Error("Orphaned() = true, want false")
	}
	if !k.PermissionsOK() {
		t.Error("PermissionsOK() = false for a 0600 key")
	}
	if k.Type != "ssh-ed25519" {
		t.Errorf("Type = %q", k.Type)
	}
	if k.Comment != "fixture@example.com" {
		t.Errorf("Comment = %q", k.Comment)
	}
	if k.Fingerprint != fixtureFP {
		t.Errorf("Fingerprint = %q", k.Fingerprint)
	}
}

func TestInspect_Orphaned(t *testing.T) {
	dir := t.TempDir()
	priv := filepath.Join(dir, "id_ed25519")

	if err := os.WriteFile(priv+".pub", []byte(fixturePub), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	k, err := Inspect(priv)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !k.Orphaned() {
		t.Error("Orphaned() = false, want true for a .pub with no private half")
	}
}

func TestInspect_Missing(t *testing.T) {
	k, err := Inspect(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("Inspect() error = %v, want nil for an absent key", err)
	}
	if k.HasPrivate || k.HasPublic {
		t.Error("absent key reported as present")
	}
}

func TestPermissionsOK(t *testing.T) {
	dir := t.TempDir()
	priv := filepath.Join(dir, "loose")

	if err := os.WriteFile(priv, []byte("PRIVATE"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	k, err := Inspect(priv)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if k.PermissionsOK() {
		t.Error("PermissionsOK() = true for a world-readable private key")
	}
}

func TestLooksPrivate(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"openssh private key", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3Blbn...\n", true},
		{"rsa private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n", true},
		{"encrypted private key", "-----BEGIN ENCRYPTED PRIVATE KEY-----\nMIIF...\n", true},
		{"public key", fixturePub, false},
		{"empty", "", false},
		{"random text", "hello world\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksPrivate([]byte(tt.data)); got != tt.want {
				t.Errorf("LooksPrivate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The real case this exists for: a private key saved with a .pub extension.
// Trusting the name would report a credential as harmless.
func TestLooksPrivate_BeatsTheFileExtension(t *testing.T) {
	dir := t.TempDir()
	misnamed := filepath.Join(dir, "personal_github.pub")

	if err := os.WriteFile(misnamed, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nb3Blbn\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	data, err := os.ReadFile(misnamed)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !LooksPrivate(data) {
		t.Error("a private key named .pub was not recognised as private")
	}
	if LooksPublic(data) {
		t.Error("a private key named .pub was reported as public")
	}
}

func TestLooksPublic(t *testing.T) {
	if !LooksPublic([]byte(fixturePub)) {
		t.Error("a real public key was not recognised")
	}
	if LooksPublic([]byte("not a key at all\n")) {
		t.Error("arbitrary text was reported as a public key")
	}
}

// FIDO hardware keys. Their algorithm names start with sk-, so a check for
// "ssh-" or "ecdsa-" alone misses exactly the keys that the most
// security-conscious users carry.
func TestLooksPublic_FIDOKeys(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{
			name: "sk-ssh-ed25519",
			data: "sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QDBwZW5zc2guY29tAAAA user@host\n",
			want: true,
		},
		{
			name: "sk-ecdsa-sha2-nistp256",
			data: "sk-ecdsa-sha2-nistp256@openssh.com AAAAInNrLWVjZHNhLXNoYTItbmlzdHAyNTZAb3Bl user@host\n",
			want: true,
		},
		{name: "ssh-ed25519", data: fixturePub, want: true},
		{
			name: "ssh-rsa",
			data: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQ user@host\n",
			want: true,
		},
		{
			name: "ecdsa-sha2-nistp256",
			data: "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY user@host\n",
			want: true,
		},
		{
			name: "a certificate is still a public key line",
			data: "ssh-ed25519-cert-v01@openssh.com AAAAIHNzaC1lZDI1NTE5LWNlcnQ user@host\n",
			want: true,
		},
		{name: "not a key", data: "hello there\n", want: false},
		{
			name: "a private key is never public, whatever it is named",
			data: "-----BEGIN OPENSSH PRIVATE KEY-----\nb3Blbn\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksPublic([]byte(tt.data)); got != tt.want {
				t.Errorf("LooksPublic() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Fingerprinting must work for a FIDO key too, since check prints it.
func TestFingerprint_FIDOKey(t *testing.T) {
	const pub = "sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QDBwZW5zc2guY29tAAAA user@host\n"

	got, err := Fingerprint([]byte(pub))
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	if !strings.HasPrefix(got, "SHA256:") {
		t.Errorf("Fingerprint() = %q, want a SHA256: prefix", got)
	}
}

// Whether the user accepted the passphrase prompt is not knowable from the
// command, so apply reads it off the file. If this were wrong, an encrypted key
// would skip the agent and verification would call a working setup broken.
func TestIsEncrypted(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	dir := t.TempDir()

	plain := filepath.Join(dir, "plain")
	locked := filepath.Join(dir, "locked")
	keygen(t, plain, "")
	keygen(t, locked, "a real passphrase")

	got, err := IsEncrypted(plain)
	if err != nil {
		t.Fatalf("IsEncrypted(plain) error = %v", err)
	}
	if got {
		t.Error("IsEncrypted(plain) = true, want false")
	}

	got, err = IsEncrypted(locked)
	if err != nil {
		t.Fatalf("IsEncrypted(locked) error = %v", err)
	}
	if !got {
		t.Error("IsEncrypted(locked) = false, want true")
	}
}

func TestIsEncrypted_Errors(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file", func(t *testing.T) {
		if _, err := IsEncrypted(filepath.Join(dir, "absent")); err == nil {
			t.Error("error = nil, want one for a missing file")
		}
	})

	tests := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"not base64", "-----BEGIN OPENSSH PRIVATE KEY-----\n!!!not base64!!!\n-----END OPENSSH PRIVATE KEY-----\n"},
		{"truncated after the magic", "-----BEGIN OPENSSH PRIVATE KEY-----\n" + "b3BlbnNzaC1rZXktdjEA" + "\n-----END OPENSSH PRIVATE KEY-----\n"},
		{"a public key, not a private one", fixturePub},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, "k-"+tt.name)
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("setup: %v", err)
			}
			if _, err := IsEncrypted(path); !errors.Is(err, ErrMalformedPrivateKey) {
				t.Errorf("error = %v, want ErrMalformedPrivateKey", err)
			}
		})
	}
}

// Generate must honour the prompt-by-default contract, which this only checks
// in the explicit no-passphrase direction, since the other needs a terminal.
func TestGenerate_NoPassphraseWritesAnUnencryptedKey(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	path := filepath.Join(t.TempDir(), "id_ed25519_test")

	if err := Generate(context.Background(), path, "test@example.com", true); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	k, err := Inspect(path)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !k.Complete() {
		t.Error("Generate() did not produce both halves")
	}
	if !k.PermissionsOK() {
		t.Errorf("generated key is mode %v, too loose for OpenSSH", k.Mode.Perm())
	}
	if k.Comment != "test@example.com" {
		t.Errorf("Comment = %q, want the comment passed in", k.Comment)
	}

	encrypted, err := IsEncrypted(path)
	if err != nil {
		t.Fatalf("IsEncrypted() error = %v", err)
	}
	if encrypted {
		t.Error("the key is encrypted despite noPassphrase being true")
	}
}

func keygen(t *testing.T, path, passphrase string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "ssh-keygen", "-t", "ed25519", "-f", path, "-N", passphrase, "-C", "test", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}
}

// A legacy RSA key made years ago is a working key, not a corrupt one. Saying
// "malformed" about it would be the wrong answer.
func TestIsEncrypted_LegacyPEM(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not installed")
	}
	dir := t.TempDir()

	plain := filepath.Join(dir, "legacy_plain")
	locked := filepath.Join(dir, "legacy_locked")
	keygenPEM(t, plain, "")
	keygenPEM(t, locked, "a real passphrase")

	got, err := IsEncrypted(plain)
	if err != nil {
		t.Fatalf("IsEncrypted(legacy plaintext) error = %v, want nil", err)
	}
	if got {
		t.Error("IsEncrypted(legacy plaintext) = true, want false")
	}

	got, err = IsEncrypted(locked)
	if err != nil {
		t.Fatalf("IsEncrypted(legacy encrypted) error = %v, want nil", err)
	}
	if !got {
		t.Error("IsEncrypted(legacy encrypted) = false, want true")
	}
}

// A format whose cipher cannot be read is distinct from a corrupt file, so a
// caller can tell the user something true.
func TestIsEncrypted_UnsupportedFormat(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "valid openssh armor, body is not an openssh key",
			content: "-----BEGIN OPENSSH PRIVATE KEY-----\naGVsbG8gdGhlcmU=\n-----END OPENSSH PRIVATE KEY-----\n",
		},
		{
			name:    "an armor line from no format we read",
			content: "-----BEGIN SOMETHING PRIVATE KEY-----\naGVsbG8=\n-----END SOMETHING PRIVATE KEY-----\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, "k-"+tt.name)
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("setup: %v", err)
			}

			_, err := IsEncrypted(path)
			if !errors.Is(err, ErrUnsupportedKeyFormat) {
				t.Errorf("error = %v, want ErrUnsupportedKeyFormat", err)
			}
			if errors.Is(err, ErrMalformedPrivateKey) {
				t.Error("a readable key in an unknown format was called malformed")
			}
		})
	}
}

// PKCS#8 announces encryption in its armor line, so both forms are answerable
// without decoding anything.
func TestIsEncrypted_PKCS8(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "unencrypted",
			content: "-----BEGIN PRIVATE KEY-----\naGVsbG8=\n-----END PRIVATE KEY-----\n",
			want:    false,
		},
		{
			name:    "encrypted",
			content: "-----BEGIN ENCRYPTED PRIVATE KEY-----\naGVsbG8=\n-----END ENCRYPTED PRIVATE KEY-----\n",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, "pkcs8-"+tt.name)
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("setup: %v", err)
			}

			got, err := IsEncrypted(path)
			if err != nil {
				t.Fatalf("IsEncrypted() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("IsEncrypted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func keygenPEM(t *testing.T, path, passphrase string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "ssh-keygen",
		"-m", "PEM", "-t", "rsa", "-b", "2048", "-f", path, "-N", passphrase, "-C", "legacy", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen -m PEM: %v\n%s", err, out)
	}
}

// On Windows every writable file reports 0666, so the Unix check flagged keys
// as too open on every run and apply never converged. Observed on a real
// machine: "fix permissions" listed again straight after a successful apply.
func TestPermissionsOK_WindowsIgnoresModeBits(t *testing.T) {
	saved := unixPermissions
	t.Cleanup(func() { unixPermissions = saved })

	k := Key{HasPrivate: true, Mode: 0o666}

	unixPermissions = false
	if !k.PermissionsOK() {
		t.Error("a 0666 key on Windows was reported as too open; the mode bits mean nothing there")
	}

	unixPermissions = true
	if k.PermissionsOK() {
		t.Error("a 0666 key on Unix was reported as fine; OpenSSH would refuse it")
	}
}
