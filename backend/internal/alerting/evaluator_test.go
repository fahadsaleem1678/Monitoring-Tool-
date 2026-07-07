package alerting

import (
	"encoding/json"
	"testing"
)

func TestEvaluatePrometheusResultFiresForVectorSample(t *testing.T) {
	data := json.RawMessage(`{
		"resultType": "vector",
		"result": [
			{"metric": {"job": "a"}, "value": [1720000000, "0"]},
			{"metric": {"job": "b"}, "value": [1720000000, "2"]}
		]
	}`)

	result, err := evaluatePrometheusResult(data, ">", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Firing {
		t.Fatal("expected alert to fire")
	}
	if result.Value == nil || *result.Value != 2 {
		t.Fatalf("value = %v, want 2", result.Value)
	}
}

func TestEvaluatePrometheusResultUsesLowestBreachingValueForLessThan(t *testing.T) {
	data := json.RawMessage(`{
		"resultType": "vector",
		"result": [
			{"metric": {"job": "a"}, "value": [1720000000, "0.9"]},
			{"metric": {"job": "b"}, "value": [1720000000, "0.2"]}
		]
	}`)

	result, err := evaluatePrometheusResult(data, "<", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Firing {
		t.Fatal("expected alert to fire")
	}
	if result.Value == nil || *result.Value != 0.2 {
		t.Fatalf("value = %v, want 0.2", result.Value)
	}
}

func TestEvaluatePrometheusResultTreatsEmptyVectorAsResolved(t *testing.T) {
	data := json.RawMessage(`{"resultType": "vector", "result": []}`)

	result, err := evaluatePrometheusResult(data, ">", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Firing {
		t.Fatal("expected empty vector to be resolved")
	}
	if result.Value != nil {
		t.Fatalf("value = %v, want nil", result.Value)
	}
}

func TestEvaluatePrometheusResultSupportsScalar(t *testing.T) {
	data := json.RawMessage(`{"resultType": "scalar", "result": [1720000000, "1"]}`)

	result, err := evaluatePrometheusResult(data, "==", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Firing {
		t.Fatal("expected scalar alert to fire")
	}
}
