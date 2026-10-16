package dataroot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestResolveRejectsEmptyPathRegardlessOfEnvironment(t *testing.T) {
	t.Setenv("PRAXIS_DATA_ROOT", t.TempDir())
	if _, err := Resolve(""); err == nil {
		t.Fatal("expected an error for an empty data root")
	}
}

func TestResolvePaths(t *testing.T) {
	t.Setenv("PRAXIS_DATA_ROOT", t.TempDir())
	for _, input := range []string{
		"relative-data",
		filepath.Join(t.TempDir(), "unused", "..", "data"),
		" data with spaces ",
		" ",
	} {
		t.Run(input, func(t *testing.T) {
			want, err := filepath.Abs(input)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Resolve(input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Root != want {
				t.Fatalf("Resolve(%q).Root = %q, want %q", input, got.Root, want)
			}
		})
	}
}

func TestResolveRejectsFilesystemRoot(t *testing.T) {
	root := t.TempDir()
	for filepath.Dir(root) != root {
		root = filepath.Dir(root)
	}
	if _, err := Resolve(root); err == nil {
		t.Fatalf("expected an error for filesystem root %q", root)
	}
}

func TestResolveLayoutDoesNotCreateDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	got, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	want := DataRoot{
		Root:                 root,
		Config:               filepath.Join(root, "config"),
		Projects:             filepath.Join(root, "projects"),
		Runtime:              filepath.Join(root, "runtime"),
		Documents:            filepath.Join(root, "runtime", "documents"),
		Attachments:          filepath.Join(root, "runtime", "attachments"),
		Temporary:            filepath.Join(root, "runtime", "tmp"),
		ModelProvidersConfig: filepath.Join(root, "config", "models.json"),
		ModelCredentialsFile: filepath.Join(root, "config", "auth.json"),
		ToolConfig:           filepath.Join(root, "config", "tools.json"),
		AgentDefinitions:     filepath.Join(root, "config", "agents"),
	}
	if got != want {
		t.Fatalf("Resolve() = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Resolve should not create directories: %v", err)
	}
}

func TestInitializeCreatesPrivateDirectories(t *testing.T) {
	d, err := Resolve(filepath.Join(t.TempDir(), " data "))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := d.Initialize(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		d.Root,
		d.Config,
		d.Projects,
		d.Runtime,
		d.Documents,
		d.AgentDefinitions,
		d.Attachments,
		d.Temporary,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Fatalf("%q is not a directory", path)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
			t.Fatalf("%q permissions = %o, want 700", path, info.Mode().Perm())
		}
	}
	for _, path := range []string{d.ModelProvidersConfig, d.ModelCredentialsFile, d.ToolConfig} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Initialize should not create config file %q: %v", path, err)
		}
	}
}

func TestInitializeRejectsEmptyRequiredPaths(t *testing.T) {
	for _, field := range []string{
		"Root",
		"Config",
		"Projects",
		"Runtime",
		"Documents",
		"AgentDefinitions",
		"Attachments",
		"Temporary",
	} {
		t.Run(field, func(t *testing.T) {
			d, err := Resolve(filepath.Join(t.TempDir(), "data"))
			if err != nil {
				t.Fatal(err)
			}
			reflect.ValueOf(&d).Elem().FieldByName(field).SetString("")
			if err := d.Initialize(context.Background()); err == nil {
				t.Fatalf("expected an error for an empty %s", field)
			}
		})
	}
}

func TestInitializePreservesWhitespaceOnlyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	d, err := Resolve(" ")
	if err != nil {
		t.Fatal(err)
	}
	d.Root = " "
	if err := d.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize should preserve a nonempty path: %v", err)
	}
	if info, err := os.Stat(" "); err != nil || !info.IsDir() {
		t.Fatalf("expected the whitespace-named directory, got info %v, err %v", info, err)
	}
}

func TestInitializeRejectsInvalidContextBeforeCreatingDirectories(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tt := range []struct {
		name string
		ctx  context.Context
		want error
	}{
		{name: "nil"},
		{name: "cancelled", ctx: ctx, want: context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Resolve(filepath.Join(t.TempDir(), "data"))
			if err != nil {
				t.Fatal(err)
			}
			err = d.Initialize(tt.ctx)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("Initialize() error = %v, want %v", err, tt.want)
			}
			if _, err := os.Stat(d.Root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid context should not create directories: %v", err)
			}
		})
	}
}

func TestInitializePreservesFilesystemError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := Resolve(filepath.Join(path, "data"))
	if err != nil {
		t.Fatal(err)
	}
	var pathErr *os.PathError
	if err := d.Initialize(context.Background()); !errors.As(err, &pathErr) {
		t.Fatalf("expected a wrapped filesystem error, got %v", err)
	}
}
