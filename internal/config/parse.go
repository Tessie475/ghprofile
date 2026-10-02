package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// ErrEmpty is returned when the profiles document contains nothing to decode.
var ErrEmpty = errors.New("profiles file is empty")

// Parse decodes a profiles document from YAML without validating its contents.
func Parse(data []byte) (*Profiles, error) {
	var profileDataCheck Profiles

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	if err := dec.Decode(&profileDataCheck); err != nil {
		// A stream with no documents decodes to io.EOF, not a syntax error.
		if errors.Is(err, io.EOF) {
			return nil, ErrEmpty
		}
		return nil, fmt.Errorf("parse profiles: %w", err)
	}

	return &profileDataCheck, nil
}
