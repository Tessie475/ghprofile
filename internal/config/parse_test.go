package config

import (
	"errors"
	"os"
	"testing"
)

const validDoc = `
version: 1
profiles:
  - name: personal
    host: github.com
    alias: github-personal
    key: ~/.ssh/personal_github
    user:
      name: A Person
      email: tessie@example.com
    default: true

  - name: work
    host: github.com
    alias: github-work
    key: ~/.ssh/id_ed25519_work
    user:
      name: A Person
      email: work@example.org
    dirs:
      - ~/work/
`

func TestParse_ValidDocument(t *testing.T) {
	got, err := Parse([]byte(validDoc))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("Parse() returned nil *Profiles with a nil error")
	}

	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	// Fatal: every assertion below indexes into this slice.
	if len(got.Profiles) != 2 {
		t.Fatalf("len(Profiles) = %d, want 2", len(got.Profiles))
	}

	if got.Profiles[1].Name != "work" {
		t.Errorf("Profiles[1].Name = %q, want %q", got.Profiles[1].Name, "work")
	}
	if got.Profiles[1].Host != "github.com" {
		t.Errorf("Profiles[1].Host = %q, want %q", got.Profiles[1].Host, "github.com")
	}
	if got.Profiles[1].Alias != "github-work" {
		t.Errorf("Profiles[1].Alias = %q, want %q", got.Profiles[1].Alias, "github-work")
	}
	if got.Profiles[1].Key != "~/.ssh/id_ed25519_work" {
		t.Errorf("Profiles[1].Key = %q, want the raw unexpanded path", got.Profiles[1].Key)
	}
	if got.Profiles[0].User.Email != "tessie@example.com" {
		t.Errorf("Profiles[0].User.Email = %q, want %q", got.Profiles[0].User.Email, "tessie@example.com")
	}
	if n := len(got.Profiles[1].Dirs); n != 1 {
		t.Errorf("len(Profiles[1].Dirs) = %d, want 1", n)
	}
	if !got.Profiles[0].Default {
		t.Error("Profiles[0].Default = false, want true")
	}
	// Omitted in the YAML, so it must come back as the zero value.
	if got.Profiles[1].Default {
		t.Error("Profiles[1].Default = true, want false for an omitted key")
	}
}

func TestParse_Fixture(t *testing.T) {
	data, err := os.ReadFile("testdata/profiles.yaml")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if len(got.Profiles) != 2 {
		t.Fatalf("len(Profiles) = %d, want 2", len(got.Profiles))
	}
	if got.Profiles[1].Name != "work" {
		t.Errorf("Profiles[1].Name = %q, want %q", got.Profiles[1].Name, "work")
	}
	if got.Profiles[0].Alias != "github-personal" {
		t.Errorf("Profiles[0].Alias = %q, want %q", got.Profiles[0].Alias, "github-personal")
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		// nil means any error will do.
		wantErr error
	}{
		{
			name:    "empty string",
			input:   "",
			wantErr: ErrEmpty,
		},
		{
			// Spaces and newlines only: a tab would be a YAML syntax error.
			name:    "whitespace only",
			input:   "   \n   \n",
			wantErr: ErrEmpty,
		},
		{
			name:    "comments only",
			input:   "# nothing to see\n# still nothing\n",
			wantErr: ErrEmpty,
		},
		{
			name:    "bad indentation",
			input:   "version: 1\nprofiles:\n  - name: work\n   alias: misaligned\n",
			wantErr: nil,
		},
		{
			name:    "unknown top level key",
			input:   "version: 1\nprofilez: []\n",
			wantErr: nil,
		},
		{
			name:    "unknown profile key",
			input:   "version: 1\nprofiles:\n  - name: work\n    emial: typo@example.com\n",
			wantErr: nil,
		},
		{
			name:    "version is a string",
			input:   "version: one\nprofiles: []\n",
			wantErr: nil,
		},
		{
			name:    "document is a bare list",
			input:   "- one\n- two\n",
			wantErr: nil,
		},
		{
			name:    "document is a bare string",
			input:   "just a string\n",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.input))
			if err == nil {
				t.Fatalf("Parse() error = nil, want an error (got %+v)", got)
			}
			if got != nil {
				t.Errorf("Parse() returned %+v alongside an error, want nil", got)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("Parse() error = %v, want it to wrap %v", err, tt.wantErr)
			}
		})
	}
}

func TestParse_DoesNotValidate(t *testing.T) {
	const nonsense = `
version: 99
profiles:
  - name: ""
    host: ""
    alias: ""
    key: ""
    user:
      name: ""
      email: "not-an-email"
`

	got, err := Parse([]byte(nonsense))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil: Parse must not validate", err)
	}
	if got.Version != 99 {
		t.Errorf("Version = %d, want 99 preserved as written", got.Version)
	}
	if len(got.Profiles) != 1 {
		t.Fatalf("len(Profiles) = %d, want 1", len(got.Profiles))
	}
	if got.Profiles[0].Name != "" {
		t.Errorf("Name = %q, want the empty string preserved", got.Profiles[0].Name)
	}
}

func TestParse_ErrorHasContext(t *testing.T) {
	_, err := Parse([]byte("version: 1\nprofiles:\n  - name: work\n   alias: misaligned\n"))
	if err == nil {
		t.Fatal("Parse() error = nil, want an error")
	}
	if len(err.Error()) < 20 {
		t.Errorf("error message %q is too terse to act on", err.Error())
	}
}
