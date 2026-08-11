package chat

import "testing"

func TestParseRoutedIntentAcceptsJSONObject(t *testing.T) {
	intent, err := parseRoutedIntent(`{"intent":"pod_crashloops"}`)
	if err != nil {
		t.Fatalf("parseRoutedIntent returned error: %v", err)
	}
	if intent != "pod_crashloops" {
		t.Fatalf("intent = %q, want pod_crashloops", intent)
	}
}

func TestParseRoutedIntentExtractsFencedJSON(t *testing.T) {
	intent, err := parseRoutedIntent("```json\n{\"intent\":\"unhealthy_pods\"}\n```")
	if err != nil {
		t.Fatalf("parseRoutedIntent returned error: %v", err)
	}
	if intent != "unhealthy_pods" {
		t.Fatalf("intent = %q, want unhealthy_pods", intent)
	}
}

func TestAllowedIntentRejectsUnknownIntent(t *testing.T) {
	if allowedIntent("delete_pod", []string{"pod_crashloops", IntentUnsupported}) {
		t.Fatal("allowedIntent accepted unknown intent")
	}
}
