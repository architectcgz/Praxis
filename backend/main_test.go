package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStartupDataRootPath(t *testing.T) {
	home := t.TempDir()
	homeEnvironment := "HOME"
	switch runtime.GOOS {
	case "windows":
		homeEnvironment = "USERPROFILE"
	case "plan9":
		homeEnvironment = "home"
	}
	t.Setenv(homeEnvironment, home)
	explicit := filepath.Join(t.TempDir(), "data")
	for _, tt := range []struct {
		name  string
		value string
		unset bool
		want  string
	}{
		{name: "explicit", value: explicit, want: explicit},
		{name: "unset", unset: true, want: filepath.Join(home, ".praxis")},
		{name: "empty", want: filepath.Join(home, ".praxis")},
		{name: "whitespace", value: " \t\n", want: filepath.Join(home, ".praxis")},
		{name: "preserve spaces", value: " " + explicit + " ", want: " " + explicit + " "},
		{name: "relative", value: "local-data", want: "local-data"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(dataRootEnvironment, tt.value)
			if tt.unset {
				if err := os.Unsetenv(dataRootEnvironment); err != nil {
					t.Fatal(err)
				}
			}
			got, err := startupDataRootPath()
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("startupDataRootPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStartupDataRootPathWithoutHome(t *testing.T) {
	homeEnvironment := "HOME"
	switch runtime.GOOS {
	case "windows":
		homeEnvironment = "USERPROFILE"
	case "plan9":
		homeEnvironment = "home"
	}
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		t.Skip("os.UserHomeDir supplies a default home on this platform")
	}
	t.Setenv(homeEnvironment, "")
	t.Setenv(dataRootEnvironment, "")
	if _, err := startupDataRootPath(); err == nil {
		t.Fatal("expected an error when no data root or home is available")
	}
	t.Setenv(dataRootEnvironment, "explicit-data")
	got, err := startupDataRootPath()
	if err != nil || got != "explicit-data" {
		t.Fatalf("explicit data root should not require a home: got %q, err %v", got, err)
	}
}
