package config

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadProfiles(t *testing.T) {

	// 1. Read the entire file into a byte slice ([]byte)
	profileData, err := os.ReadFile("testdata/profiles.yaml")
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// 2. Create an instance of the target struct
	var profile Profiles

	// 3. Unmarshal the file byte slice into the struct pointer
	err = yaml.Unmarshal(profileData, &profile)
	if err != nil {
		t.Fatalf("failed to unmarshal YAML: %v", err)
	}

	t.Logf("Loaded profiles: %+v\n", profile)

}
