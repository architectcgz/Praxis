package document

import (
	"context"
	"testing"
)

func TestDocumentAcceptsArbitraryJSONFieldNames(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]any{"token": "example", "nested": map[string]any{"api_key": "data", "hidden_prompt": "text"}}
	first, err := store.Put(context.Background(), "collection", "item", value)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(context.Background(), "collection", "item", value)
	if err != nil || first != second {
		t.Fatal(first, second, err)
	}
	var actual map[string]any
	if err := store.Get(context.Background(), first, &actual); err != nil || actual["token"] != "example" {
		t.Fatal(actual, err)
	}
	if _, err := store.Put(context.Background(), "../outside", "item", value); err == nil {
		t.Fatal("unsafe key accepted")
	}
}
