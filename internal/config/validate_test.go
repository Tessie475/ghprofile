package config

import (
	"errors"
	"strings"
	"testing"
)

func goodProfile() Profile {
	return Profile{
		Name:  "work",
		Host:  "github.com",
		Alias: "github-work",
		Key:   "/home/u/.ssh/id_ed25519_work",
		User:  User{Name: "A Person", Email: "person@example.com"},
		Dirs:  []string{"/home/u/work/"},
	}
}

func goodFile() *Profiles {
	personal := goodProfile()
	personal.Name = "personal"
	personal.Alias = "github-personal"
	personal.Key = "/home/u/.ssh/personal"
	personal.Dirs = nil
	personal.Default = true

	return &Profiles{Version: Version, Profiles: []Profile{personal, goodProfile()}}
}

func TestProfile_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Profile)
		wantErr error
	}{
		{name: "valid", mutate: func(*Profile) {}},
		{name: "valid with no dirs", mutate: func(p *Profile) { p.Dirs = nil }},
		{name: "missing name", mutate: func(p *Profile) { p.Name = "" }, wantErr: ErrMissingField},
		{name: "whitespace name", mutate: func(p *Profile) { p.Name = "   " }, wantErr: ErrMissingField},
		{name: "missing host", mutate: func(p *Profile) { p.Host = "" }, wantErr: ErrMissingField},
		{name: "missing alias", mutate: func(p *Profile) { p.Alias = "" }, wantErr: ErrMissingField},
		{name: "missing key", mutate: func(p *Profile) { p.Key = "" }, wantErr: ErrMissingField},
		{name: "missing user name", mutate: func(p *Profile) { p.User.Name = "" }, wantErr: ErrMissingField},
		{name: "missing user email", mutate: func(p *Profile) { p.User.Email = "" }, wantErr: ErrMissingField},
		{name: "blank dir entry", mutate: func(p *Profile) { p.Dirs = []string{" "} }, wantErr: ErrMissingField},
		{name: "alias with space", mutate: func(p *Profile) { p.Alias = "github work" }, wantErr: ErrInvalidAlias},
		{name: "alias with star", mutate: func(p *Profile) { p.Alias = "github-*" }, wantErr: ErrInvalidAlias},
		{name: "alias with question mark", mutate: func(p *Profile) { p.Alias = "github?" }, wantErr: ErrInvalidAlias},
		{name: "email without at", mutate: func(p *Profile) { p.User.Email = "person.example.com" }, wantErr: ErrInvalidEmail},
		{name: "email without local part", mutate: func(p *Profile) { p.User.Email = "@example.com" }, wantErr: ErrInvalidEmail},
		{name: "email without domain", mutate: func(p *Profile) { p.User.Email = "person@" }, wantErr: ErrInvalidEmail},
		{name: "email with two at signs", mutate: func(p *Profile) { p.User.Email = "a@b@c" }, wantErr: ErrInvalidEmail},
		{name: "email with space", mutate: func(p *Profile) { p.User.Email = "a person@example.com" }, wantErr: ErrInvalidEmail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := goodProfile()
			tt.mutate(&p)

			err := p.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want it to wrap %v", err, tt.wantErr)
			}
		})
	}
}

func TestProfile_ValidateNamesTheProfile(t *testing.T) {
	p := goodProfile()
	p.User.Email = "nope"

	err := p.Validate()
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "work") {
		t.Errorf("error %q does not name the profile", err.Error())
	}
}

func TestProfile_ValidateReportsEveryProblem(t *testing.T) {
	p := goodProfile()
	p.Name = ""
	p.Alias = "bad alias"
	p.User.Email = "nope"

	err := p.Validate()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []error{ErrMissingField, ErrInvalidAlias, ErrInvalidEmail} {
		if !errors.Is(err, want) {
			t.Errorf("%v was not reported", want)
		}
	}
}

func TestProfiles_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Profiles)
		wantErr error
	}{
		{name: "valid", mutate: func(*Profiles) {}},
		{name: "no default is allowed", mutate: func(f *Profiles) { f.Profiles[0].Default = false }},
		{name: "version zero", mutate: func(f *Profiles) { f.Version = 0 }, wantErr: ErrUnsupportedVersion},
		{name: "future version", mutate: func(f *Profiles) { f.Version = 2 }, wantErr: ErrUnsupportedVersion},
		{name: "no profiles", mutate: func(f *Profiles) { f.Profiles = nil }, wantErr: ErrNoProfiles},
		{
			name:    "duplicate name",
			mutate:  func(f *Profiles) { f.Profiles[1].Name = f.Profiles[0].Name },
			wantErr: ErrDuplicateName,
		},
		{
			name:    "duplicate name differing by case",
			mutate:  func(f *Profiles) { f.Profiles[1].Name = strings.ToUpper(f.Profiles[0].Name) },
			wantErr: ErrDuplicateName,
		},
		{
			name:    "duplicate alias",
			mutate:  func(f *Profiles) { f.Profiles[1].Alias = f.Profiles[0].Alias },
			wantErr: ErrDuplicateAlias,
		},
		{
			name: "duplicate dir",
			mutate: func(f *Profiles) {
				f.Profiles[0].Dirs = []string{"/home/u/work/"}
				f.Profiles[1].Dirs = []string{"/home/u/work"}
			},
			wantErr: ErrDuplicateDir,
		},
		{
			name:    "two defaults",
			mutate:  func(f *Profiles) { f.Profiles[1].Default = true },
			wantErr: ErrMultipleDefaults,
		},
		{
			name:    "a profile is itself invalid",
			mutate:  func(f *Profiles) { f.Profiles[1].User.Email = "" },
			wantErr: ErrMissingField,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := goodFile()
			tt.mutate(f)

			err := f.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want it to wrap %v", err, tt.wantErr)
			}
		})
	}
}

func TestProfile_ValidateOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    map[string]string
		wantErr error
	}{
		{name: "valid", opts: map[string]string{"ServerAliveInterval": "60"}},
		{name: "no options at all", opts: nil},
		{name: "blank key", opts: map[string]string{"  ": "60"}, wantErr: ErrMissingField},
		{name: "blank value", opts: map[string]string{"ServerAliveInterval": " "}, wantErr: ErrMissingField},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := goodProfile()
			p.Options = tt.opts

			err := p.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// Two profiles sharing a private key cannot authenticate as different
// accounts, which makes it the one mistake that silently defeats the tool.
func TestProfiles_ValidateRejectsASharedKey(t *testing.T) {
	f := goodFile()
	f.Profiles[1].Key = f.Profiles[0].Key

	err := f.Validate()
	if !errors.Is(err, ErrDuplicateKey) {
		t.Errorf("Validate() error = %v, want it to wrap %v", err, ErrDuplicateKey)
	}
}
