package request

import "testing"

func TestProviderKeyInputBoundary(t *testing.T) {
	value, err := NewSetProviderKey(" provider ", " key ")
	if err != nil || value.ProviderID != "provider" || value.Key != " key " {
		t.Fatal(value, err)
	}
	if _, err := NewSetProviderKey("provider", " \n\t"); err == nil {
		t.Fatal("blank key accepted")
	}
	if value, err := NewSetProviderKey("provider", ""); err != nil || value.Key != "" {
		t.Fatal(value, err)
	}
}
