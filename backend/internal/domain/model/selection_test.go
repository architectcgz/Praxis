package model

import "testing"

func TestNewModelSelectionNormalizesFields(t *testing.T) {
	selection, err := NewModelSelection(" provider ", " model ", " high ")
	if err != nil {
		t.Fatal(err)
	}
	if selection.ProviderID != "provider" || selection.ModelID != "model" || selection.ReasoningLevel != "high" {
		t.Fatalf("unexpected selection: %#v", selection)
	}
}

func TestModelSelectionValidateRejectsNonCanonicalFields(t *testing.T) {
	selection := ModelSelection{ProviderID: " provider", ModelID: "model"}
	if err := selection.Validate(); err == nil {
		t.Fatal("expected non-canonical selection to fail validation")
	}
}
