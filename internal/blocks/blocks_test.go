package blocks

import (
	"errors"
	"strings"
	"testing"
)

const handWritten = `# Work account
Host github-work
  HostName github.com

# Personal account
Host github-personal
  HostName github.com
`

func TestUpsert_AppendsWhenAbsent(t *testing.T) {
	got, err := Upsert(handWritten, "work", "Host github-work\n")
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if !strings.HasPrefix(got, handWritten) {
		t.Error("existing content was modified, it must be preserved exactly")
	}
	if !strings.Contains(got, "# BEGIN ghprofile:work") || !strings.Contains(got, "# END ghprofile:work") {
		t.Error("markers missing from the appended block")
	}
}

func TestUpsert_ReplacesInPlace(t *testing.T) {
	content := "before\n# BEGIN ghprofile:work\nold\n# END ghprofile:work\nafter\n"

	got, err := Upsert(content, "work", "new\n")
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	want := "before\n# BEGIN ghprofile:work\nnew\n# END ghprofile:work\nafter\n"
	if got != want {
		t.Errorf("Upsert() =\n%q\nwant\n%q", got, want)
	}
}

func TestUpsert_IsIdempotent(t *testing.T) {
	once, err := Upsert(handWritten, "work", "body\n")
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	twice, err := Upsert(once, "work", "body\n")
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if once != twice {
		t.Error("applying the same block twice changed the file")
	}
}

func TestUpsert_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"empty file", ""},
		{"no trailing newline", "Host a\n  HostName b"},
		{"crlf line endings", "Host a\r\n  HostName b\r\n"},
		{"only a comment", "# nothing here\n"},
		{"blank lines at end", "Host a\n\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Upsert(tt.content, "work", "line\n")
			if err != nil {
				t.Fatalf("Upsert() error = %v", err)
			}
			body, ok, err := Find(got, "work")
			if err != nil || !ok {
				t.Fatalf("Find() after Upsert: ok=%v err=%v", ok, err)
			}
			if body != "line" {
				t.Errorf("body = %q, want %q", body, "line")
			}
		})
	}
}

func TestRemove(t *testing.T) {
	content := "keep me\n\n# BEGIN ghprofile:work\nbody\n# END ghprofile:work\nkeep me too\n"

	got, err := Remove(content, "work")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if strings.Contains(got, "ghprofile:work") {
		t.Error("block survived removal")
	}
	if !strings.Contains(got, "keep me") || !strings.Contains(got, "keep me too") {
		t.Error("surrounding content was lost")
	}
}

func TestRemove_AbsentIsNotAnError(t *testing.T) {
	got, err := Remove(handWritten, "nope")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if got != handWritten {
		t.Error("removing an absent block changed the file")
	}
}

func TestScanErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr error
	}{
		{
			name:    "unterminated block",
			content: "# BEGIN ghprofile:work\nbody\n",
			wantErr: ErrUnterminated,
		},
		{
			name:    "end without begin",
			content: "# END ghprofile:work\n",
			wantErr: ErrUnterminated,
		},
		{
			name:    "nested begin",
			content: "# BEGIN ghprofile:a\n# BEGIN ghprofile:b\n# END ghprofile:b\n",
			wantErr: ErrUnterminated,
		},
		{
			name:    "mismatched end",
			content: "# BEGIN ghprofile:a\n# END ghprofile:b\n",
			wantErr: ErrUnterminated,
		},
		{
			name:    "duplicate block",
			content: "# BEGIN ghprofile:a\n# END ghprofile:a\n# BEGIN ghprofile:a\n# END ghprofile:a\n",
			wantErr: ErrDuplicate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Upsert(tt.content, "x", "y\n"); !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want it to wrap %v", err, tt.wantErr)
			}
		})
	}
}

func TestList(t *testing.T) {
	content := "# BEGIN ghprofile:a\n# END ghprofile:a\n# BEGIN ghprofile:b\n# END ghprofile:b\n"

	got, err := List(content)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("List() = %v, want [a b]", got)
	}
}

// Nothing outside the markers may change, byte for byte.
func TestUpsert_PreservesForeignContentExactly(t *testing.T) {
	const foreign = "Host bastion\n  # a comment with odd    spacing\n\tProxyJump x\n\n"

	got, err := Upsert(foreign, "work", "body\n")
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	stripped, err := Remove(got, "work")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if stripped != foreign {
		t.Errorf("round trip changed foreign content:\ngot  %q\nwant %q", stripped, foreign)
	}
}
