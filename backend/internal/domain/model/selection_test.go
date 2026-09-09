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

func TestModelSelectAppliesConfiguredDefaultReasoning(t *testing.T) {
	model, err := NewModel("model", 128000, 8192, []string{"low", "medium", "high"}, "medium")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := model.Select("provider", "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Selection.ProviderID != "provider" || selected.Selection.ModelID != "model" ||
		selected.Selection.ReasoningLevel != "medium" || selected.ContextWindow != 128000 || selected.MaxOutputTokens != 8192 {
		t.Fatalf("unexpected selected model: %#v", selected)
	}
}

func TestModelSelectRejectsUnsupportedReasoning(t *testing.T) {
	model, err := NewModel("model", 128000, 8192, []string{"low"}, "low")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.Select("provider", "high"); err == nil {
		t.Fatal("expected unsupported reasoning to fail")
	}
}

func TestModelWithoutReasoningRejectsRequestedReasoning(t *testing.T) {
	model, err := NewModel("model", 128000, 8192, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.Select("provider", "low"); err == nil {
		t.Fatal("expected unsupported reasoning to fail")
	}
}
