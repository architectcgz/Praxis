package document

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorePublishesContentAddressedDocument(t *testing.T) {
	store := NewMemory()
	ref, err := store.Put(context.Background(), "execution", "execution_test", map[string]string{"state": "running"})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := store.Get(context.Background(), ref, &value); err != nil {
		t.Fatal(err)
	}
	if value["state"] != "running" {
		t.Fatalf("state=%q", value["state"])
	}
	if _, err := store.Put(context.Background(), "execution", "execution_test", map[string]string{"token": "secret"}); err == nil {
		t.Fatal("sensitive document was accepted")
	}
	if !errors.Is(store.Get(context.Background(), "execution/execution_test/sha256-missing.json", &value), os.ErrNotExist) {
		t.Fatal("missing document did not return os.ErrNotExist")
	}
}

func TestStorePublishesFilesystemDocumentWithoutPlatformUnsafeCharacters(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "documents"))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put(context.Background(), "execution", "execution_test", map[string]string{"state": "settled"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ref, ":") {
		t.Fatalf("document reference contains a platform-unsafe colon: %q", ref)
	}
	var value map[string]string
	if err := store.Get(context.Background(), ref, &value); err != nil {
		t.Fatal(err)
	}
	if value["state"] != "settled" {
		t.Fatalf("state=%q", value["state"])
	}
}
