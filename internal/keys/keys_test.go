package keys

import (
	"errors"
	"os"
	"path/filepath"
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
