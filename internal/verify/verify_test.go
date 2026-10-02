package verify

import "testing"

func TestParseGreeting(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantOK   bool
	}{
		{
			name:     "github",
			input:    "Hi Tessie475! You've successfully authenticated, but GitHub does not provide shell access.\n",
			wantName: "Tessie475",
			wantOK:   true,
		},
		{
			name:     "gitlab",
			input:    "Welcome to GitLab, @someone!\nHi someone! Welcome.\n",
			wantName: "someone",
			wantOK:   true,
		},
		{
			name:     "org account with punctuation",
			input:    "Hi some-org/deploy-key! You've successfully authenticated.\n",
			wantName: "some-org/deploy-key",
			wantOK:   true,
		},
		{
			name:   "permission denied",
			input:  "git@github.com: Permission denied (publickey).\n",
			wantOK: false,
		},
		{name: "empty", input: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseGreeting(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.wantName {
				t.Errorf("name = %q, want %q", got, tt.wantName)
			}
		})
	}
}
