package workspacefs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"praxis/internal/contracts"
	"testing"
)

func TestReadTextGenericErrorsAndRootBoundary(t *testing.T) {
	root := t.TempDir()
	fs := Filesystem{}
	for _, tc := range []struct {
		name  string
		data  []byte
		limit int64
		want  error
	}{
		{
			"large",
			[]byte("12345"),
			4,
			contracts.ErrFileTooLarge,
		},
		{
			"binary",
			[]byte{0, 1},
			10,
			contracts.ErrInvalidText,
		},
		{
			"invalid",
			[]byte{255},
			10,
			contracts.ErrInvalidText,
		},
	} {
		if err := os.WriteFile(filepath.Join(root, tc.name), tc.data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := fs.ReadText(context.Background(), root, tc.name, tc.limit); !errors.Is(err, tc.want) {
			t.Fatal(tc.name, err)
		}
	}
	if _, err := fs.ReadText(context.Background(), root, ".", 10); !errors.Is(err, contracts.ErrNotRegularFile) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "text"), []byte("\xEF\xBB\xBF中文"), 0600); err != nil {
		t.Fatal(err)
	}
	if text, err := fs.ReadText(context.Background(), root, "text", 30); err != nil || text != "中文" {
		t.Fatal(text, err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if _, err := fs.ReadText(context.Background(), root, "link", 100); err == nil {
		t.Fatal("symlink escaped workspace")
	}
}
