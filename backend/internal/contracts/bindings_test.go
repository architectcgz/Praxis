package contracts

import (
	"encoding/json"
	"testing"
)

func TestModelConfigDocumentExposesSecretPresenceOnly(t *testing.T) {
	encoded, err := json.Marshal(ModelConfigDocument{Providers: []ProviderConfigOption{{
		ID: "provider", ProviderName: "Provider", HasAPIKey: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Providers) != 1 || document.Providers[0]["hasAPIKey"] != true {
		t.Fatalf("secret presence is missing: %s", encoded)
	}
	if _, exposed := document.Providers[0]["apiKey"]; exposed {
		t.Fatalf("secret value field is exposed: %s", encoded)
	}
}
