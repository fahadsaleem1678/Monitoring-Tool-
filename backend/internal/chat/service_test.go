package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type fakeQuerier struct {
	results map[string]json.RawMessage
}

type fakeKubernetes struct {
	namespaces []Namespace
	pods       []Pod
	podsErr    error
	events     []Event
	logs       Logs
}

type fakeIntentRouter struct {
	intent  string
	err     error
	message string
	intents []string
}

func (f fakeQuerier) InstantQuery(_ context.Context, query string) (json.RawMessage, error) {
	result, ok := f.results[query]
	if !ok {
		return nil, fmt.Errorf("unexpected query %s", query)
	}
	return result, nil
}

func (f fakeKubernetes) Namespaces(_ context.Context) ([]Namespace, error) {
	return f.namespaces, nil
}

func (f fakeKubernetes) Pods(_ context.Context, _ string) ([]Pod, error) {
	if f.podsErr != nil {
		return nil, f.podsErr
	}
	return f.pods, nil
}

func (f fakeKubernetes) Events(_ context.Context, _ string) ([]Event, error) {
	return f.events, nil
}

func (f fakeKubernetes) Logs(_ context.Context, _, _ string, _ int) (Logs, error) {
	return f.logs, nil
}

func (f *fakeIntentRouter) Route(_ context.Context, message string, _ Context, intents []string) (string, error) {
	f.message = message
	f.intents = intents
	return f.intent, f.err
}

func TestServiceAskMatchesCrashLoops(t *testing.T) {
	service := NewService(fakeQuerier{
		results: map[string]json.RawMessage{
			`sum by (pod) (kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})`: vectorPayload(
				sample(map[string]string{"pod": "broken-api-5d8c"}, "1"),
				sample(map[string]string{"pod": "broken-worker-7f9b"}, "1"),
			),
		},
	}, "monitoring-tool")

	response, err := service.Ask(context.Background(), "are there any crash loops?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "pod_crashloops" {
		t.Fatalf("intent = %q, want pod_crashloops", response.Intent)
	}
	if response.Confidence != ConfidenceHigh {
		t.Fatalf("confidence = %q, want %q", response.Confidence, ConfidenceHigh)
	}
	if !strings.Contains(response.Answer, "2") || !strings.Contains(response.Answer, "CrashLoopBackOff") {
		t.Fatalf("answer %q did not include crash loop count", response.Answer)
	}
	if !strings.Contains(response.Answer, "broken-api-5d8c") || !strings.Contains(response.Answer, "broken-worker-7f9b") {
		t.Fatalf("answer %q did not include affected pod names", response.Answer)
	}
	if !hasFact(response.Facts, "Pod", "broken-api-5d8c") || !hasFact(response.Facts, "Pod", "broken-worker-7f9b") {
		t.Fatalf("facts = %#v, want affected pod facts", response.Facts)
	}
	if !containsString(response.Suggestions, "Show details for broken-api-5d8c") {
		t.Fatalf("suggestions = %#v, want pod detail suggestion", response.Suggestions)
	}
	if len(response.Queries) != 1 {
		t.Fatalf("queries length = %d, want 1", len(response.Queries))
	}
}

func TestServiceAskMatchesCrashBackLoopTypo(t *testing.T) {
	service := NewService(fakeQuerier{
		results: map[string]json.RawMessage{
			`sum by (pod) (kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})`: vectorPayload(
				sample(map[string]string{"pod": "broken-api-5d8c"}, "1"),
			),
		},
	}, "monitoring-tool")

	response, err := service.Ask(context.Background(), "which pods are in crash back loop")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "pod_crashloops" {
		t.Fatalf("intent = %q, want pod_crashloops", response.Intent)
	}
	if !strings.Contains(response.Answer, "broken-api-5d8c") {
		t.Fatalf("answer %q did not include affected pod name", response.Answer)
	}
}

func TestServiceAskReturnsUnsupportedForUnknownQuestion(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool")

	response, err := service.Ask(context.Background(), "can you restart the api deployment?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != IntentUnsupported {
		t.Fatalf("intent = %q, want unsupported", response.Intent)
	}
	if response.Confidence != ConfidenceLow {
		t.Fatalf("confidence = %q, want low", response.Confidence)
	}
	if len(response.Queries) != 0 {
		t.Fatalf("queries length = %d, want 0", len(response.Queries))
	}
	if len(response.Suggestions) == 0 {
		t.Fatal("expected fallback suggestions")
	}
}

func TestServiceAskUsesLLMRouterForUnknownReadOnlyQuestion(t *testing.T) {
	router := &fakeIntentRouter{intent: "pod_crashloops"}
	service := NewService(fakeQuerier{
		results: map[string]json.RawMessage{
			`sum by (pod) (kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})`: vectorPayload(
				sample(map[string]string{"pod": "broken-api-5d8c"}, "1"),
			),
		},
	}, "monitoring-tool").WithIntentRouter(router)

	response, err := service.Ask(context.Background(), "is anything repeatedly dying?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if router.message != "is anything repeatedly dying?" {
		t.Fatalf("router message = %q, want original message", router.message)
	}
	if !containsString(router.intents, "pod_crashloops") || !containsString(router.intents, IntentUnsupported) {
		t.Fatalf("router intents = %#v, want approved intents", router.intents)
	}
	if response.Intent != "pod_crashloops" {
		t.Fatalf("intent = %q, want pod_crashloops", response.Intent)
	}
	if response.Engine != "llm-routed" {
		t.Fatalf("engine = %q, want llm-routed", response.Engine)
	}
}

func TestServiceAskDoesNotRunUnsupportedLLMRoute(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithIntentRouter(&fakeIntentRouter{intent: IntentUnsupported})

	response, err := service.Ask(context.Background(), "please restart the api")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != IntentUnsupported {
		t.Fatalf("intent = %q, want unsupported", response.Intent)
	}
	if len(response.Queries) != 0 {
		t.Fatalf("queries length = %d, want 0", len(response.Queries))
	}
}

func TestServiceAskFallsBackWhenLLMRouterFails(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithIntentRouter(&fakeIntentRouter{err: fmt.Errorf("timeout")})

	response, err := service.Ask(context.Background(), "is anything repeatedly dying?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != IntentUnsupported {
		t.Fatalf("intent = %q, want unsupported", response.Intent)
	}
	if response.Engine != "llm-unavailable" {
		t.Fatalf("engine = %q, want llm-unavailable", response.Engine)
	}
}

func TestServiceAskMatchesSingularPodRestartQuestion(t *testing.T) {
	service := NewService(fakeQuerier{
		results: map[string]json.RawMessage{
			`sum by (pod) (increase(kube_pod_container_status_restarts_total{namespace="monitoring-tool"}[5m]))`: vectorPayload(),
		},
	}, "monitoring-tool")

	response, err := service.Ask(context.Background(), "any pod with 0 restart?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "pod_restarts" {
		t.Fatalf("intent = %q, want pod_restarts", response.Intent)
	}
	if !strings.Contains(response.Answer, "No pod restarts") {
		t.Fatalf("answer = %q, want restart zero-value wording", response.Answer)
	}
}

func TestServiceAskFormatsHealthyZeroValue(t *testing.T) {
	service := NewService(fakeQuerier{
		results: map[string]json.RawMessage{
			`sum by (pod) (kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})`: vectorPayload(),
		},
	}, "monitoring-tool")

	response, err := service.Ask(context.Background(), "any crashloops")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if !strings.Contains(response.Answer, "No pods") {
		t.Fatalf("answer = %q, want healthy zero-value wording", response.Answer)
	}
	if response.Facts[0].Severity != "healthy" {
		t.Fatalf("severity = %q, want healthy", response.Facts[0].Severity)
	}
}

func TestServiceAskRejectsEmptyAndLongMessages(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool")

	if _, err := service.Ask(context.Background(), "  "); err == nil {
		t.Fatal("expected empty message error")
	}

	if _, err := service.Ask(context.Background(), strings.Repeat("x", maxMessageLength+1)); err == nil {
		t.Fatal("expected long message error")
	}
}

func TestServiceAskFindsPodNamedQuestion(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{{Name: "broken-api-5d8c", Phase: "Running"}},
	}, 80)

	response, err := service.Ask(context.Background(), "any pod named broken-api?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "pod_details" {
		t.Fatalf("intent = %q, want pod_details", response.Intent)
	}
	if !strings.Contains(response.Answer, "broken-api-5d8c") {
		t.Fatalf("answer = %q, want matched pod name", response.Answer)
	}
}

func TestServiceAskListsAllPods(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{
			{Name: "healthy-api-6c7d", Phase: "Running"},
			{Name: "broken-api-5d8c", Phase: "Running", RestartCount: 4, WaitingReasons: []string{"CrashLoopBackOff"}},
		},
	}, 80)

	response, err := service.Ask(context.Background(), "list all pods")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "all_pods" {
		t.Fatalf("intent = %q, want all_pods", response.Intent)
	}
	for _, expected := range []string{"healthy-api-6c7d", "broken-api-5d8c", "CrashLoopBackOff"} {
		if !strings.Contains(response.Answer, expected) {
			t.Fatalf("answer %q did not include %q", response.Answer, expected)
		}
	}
}

func TestServiceAskListsAllPodNames(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{{Name: "healthy-api-6c7d", Phase: "Running"}},
	}, 80)

	response, err := service.Ask(context.Background(), "show all pod names")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "all_pods" {
		t.Fatalf("intent = %q, want all_pods", response.Intent)
	}
	if !strings.Contains(response.Answer, "healthy-api-6c7d") {
		t.Fatalf("answer %q did not include pod name", response.Answer)
	}
}

func TestServiceAskListsNamespaces(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		namespaces: []Namespace{
			{Name: "default", Phase: "Active"},
			{Name: "monitoring-tool", Phase: "Active"},
			{Name: "kube-system", Phase: "Active"},
		},
	}, 80)

	response, err := service.Ask(context.Background(), "tell me all the namespaces in my cluster")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "namespaces" {
		t.Fatalf("intent = %q, want namespaces", response.Intent)
	}
	for _, expected := range []string{"default", "monitoring-tool", "kube-system"} {
		if !strings.Contains(response.Answer, expected) {
			t.Fatalf("answer %q did not include namespace %q", response.Answer, expected)
		}
		if !hasFact(response.Facts, "Namespace", expected) {
			t.Fatalf("facts = %#v, want namespace fact %q", response.Facts, expected)
		}
	}
	if len(response.Queries) != 1 || response.Queries[0] != "kubernetes.namespaces" {
		t.Fatalf("queries = %#v, want kubernetes.namespaces", response.Queries)
	}
}

func TestServiceAskListsUnhealthyPods(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{
			{Name: "healthy-api-6c7d", Phase: "Running"},
			{Name: "broken-api-5d8c", Phase: "Running", RestartCount: 4, WaitingReasons: []string{"CrashLoopBackOff"}},
			{Name: "oversized-api-9a1b", Phase: "Pending"},
		},
	}, 80)

	response, err := service.Ask(context.Background(), "which pods are failing?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "unhealthy_pods" {
		t.Fatalf("intent = %q, want unhealthy_pods", response.Intent)
	}
	for _, expected := range []string{"broken-api-5d8c", "CrashLoopBackOff", "oversized-api-9a1b", "Pending"} {
		if !strings.Contains(response.Answer, expected) {
			t.Fatalf("answer %q did not include %q", response.Answer, expected)
		}
	}
	if strings.Contains(response.Answer, "healthy-api-6c7d") {
		t.Fatalf("answer %q included healthy pod", response.Answer)
	}
}

func TestServiceAskListsUnhealthyPodsHealthyState(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{{Name: "healthy-api-6c7d", Phase: "Running"}},
	}, 80)

	response, err := service.Ask(context.Background(), "show unhealthy pods")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "unhealthy_pods" {
		t.Fatalf("intent = %q, want unhealthy_pods", response.Intent)
	}
	if !strings.Contains(response.Answer, "Pods look healthy") {
		t.Fatalf("answer = %q, want healthy pod-list wording", response.Answer)
	}
	if response.Facts[0].Severity != "healthy" {
		t.Fatalf("severity = %q, want healthy", response.Facts[0].Severity)
	}
}

func TestServiceAskPrioritizesClusterProblems(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{
			{Name: "healthy-api-6c7d", Phase: "Running"},
			{Name: "pending-api-9a1b", Phase: "Pending"},
			{Name: "bad-image-api-2c3d", Phase: "Pending", WaitingReasons: []string{"ImagePullBackOff"}},
			{Name: "broken-api-5d8c", Phase: "Running", RestartCount: 5, WaitingReasons: []string{"CrashLoopBackOff"}},
		},
	}, 80)

	response, err := service.Ask(context.Background(), "what should I fix first?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "cluster_priority_summary" {
		t.Fatalf("intent = %q, want cluster_priority_summary", response.Intent)
	}
	for _, expected := range []string{
		"Priority 1: broken-api-5d8c is in CrashLoopBackOff",
		"Priority 2: bad-image-api-2c3d has image pull failure",
		"Priority 3: pending-api-9a1b is Pending",
	} {
		if !strings.Contains(response.Answer, expected) {
			t.Fatalf("answer %q did not include %q", response.Answer, expected)
		}
	}
	if !hasFact(response.Facts, "Pod", "broken-api-5d8c") {
		t.Fatalf("facts = %#v, want priority pod fact", response.Facts)
	}
	if !containsString(response.Suggestions, "Show details for broken-api-5d8c") {
		t.Fatalf("suggestions = %#v, want pod detail suggestion", response.Suggestions)
	}
}

func TestServiceAskPrioritySummaryHealthy(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{{Name: "healthy-api-6c7d", Phase: "Running"}},
	}, 80)

	response, err := service.Ask(context.Background(), "quick health report")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "cluster_priority_summary" {
		t.Fatalf("intent = %q, want cluster_priority_summary", response.Intent)
	}
	if !strings.Contains(response.Answer, "do not see urgent pod problems") {
		t.Fatalf("answer = %q, want healthy priority wording", response.Answer)
	}
	if response.Facts[0].Severity != "healthy" {
		t.Fatalf("severity = %q, want healthy", response.Facts[0].Severity)
	}
}

func TestServiceAskPrioritySummaryFallsBackToPrometheus(t *testing.T) {
	service := NewService(fakeQuerier{
		results: map[string]json.RawMessage{
			`sum by (pod) (kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})`: vectorPayload(
				sample(map[string]string{"pod": "broken-api-5d8c"}, "1"),
			),
			`sum by (pod) (kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason=~"ImagePullBackOff|ErrImagePull"})`: vectorPayload(),
			`sum by (pod) (kube_pod_status_phase{namespace="monitoring-tool",phase="Pending"})`: vectorPayload(),
			`sum by (pod) (increase(kube_pod_container_status_restarts_total{namespace="monitoring-tool"}[5m]))`: vectorPayload(),
			`sum(kube_node_status_condition{condition="Ready",status="true"})`: vectorPayload(
				sample(map[string]string{}, "1"),
			),
		},
	}, "monitoring-tool").WithKubernetes(fakeKubernetes{podsErr: fmt.Errorf("timeout")}, 80)

	response, err := service.Ask(context.Background(), "summarize cluster problems")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "cluster_priority_summary" {
		t.Fatalf("intent = %q, want cluster_priority_summary", response.Intent)
	}
	if response.Confidence != ConfidenceLow {
		t.Fatalf("confidence = %q, want low", response.Confidence)
	}
	for _, expected := range []string{"Kubernetes pod listing timed out", "CrashLoopBackOff", "broken-api-5d8c"} {
		if !strings.Contains(response.Answer, expected) {
			t.Fatalf("answer %q did not include %q", response.Answer, expected)
		}
	}
	if strings.Contains(response.Answer, "kubernetes mcp") || strings.Contains(response.Answer, "Client.Timeout") {
		t.Fatalf("answer %q exposed raw backend error", response.Answer)
	}
	if !hasFact(response.Facts, "Priority report", "Prometheus fallback") {
		t.Fatalf("facts = %#v, want fallback fact", response.Facts)
	}
}

func TestServiceAskExplainsPodDetails(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{
			{
				Namespace:      "monitoring-tool",
				Name:           "broken-api-5d8c",
				Phase:          "Running",
				RestartCount:   5,
				WaitingReasons: []string{"CrashLoopBackOff"},
			},
		},
		events: []Event{
			{Reason: "BackOff", Message: "Back-off restarting failed container broken-api in pod broken-api-5d8c"},
		},
		logs: Logs{Pod: "broken-api-5d8c", Preview: "fatal config missing\nprocess exited"},
	}, 80)

	response, err := service.Ask(context.Background(), "why is broken-api failing?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Intent != "pod_details" {
		t.Fatalf("intent = %q, want pod_details", response.Intent)
	}
	for _, expected := range []string{"broken-api-5d8c", "CrashLoopBackOff", "BackOff", "fatal config missing"} {
		if !strings.Contains(response.Answer, expected) {
			t.Fatalf("answer %q did not include %q", response.Answer, expected)
		}
	}
	if len(response.Queries) != 3 {
		t.Fatalf("queries length = %d, want 3", len(response.Queries))
	}
}

func TestServiceAskPodDetailsSuggestsKnownPodsWhenNoMatch(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{{Name: "broken-api-5d8c"}},
	}, 80)

	response, err := service.Ask(context.Background(), "show details for unknown-api")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if response.Confidence != ConfidenceLow {
		t.Fatalf("confidence = %q, want low", response.Confidence)
	}
	if len(response.Suggestions) == 0 || !strings.Contains(response.Suggestions[0], "broken-api-5d8c") {
		t.Fatalf("suggestions = %#v, want known pod suggestion", response.Suggestions)
	}
}

func TestServiceAskUsesContextForItFollowup(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{
			{Name: "broken-api-5d8c", Phase: "Running", RestartCount: 3, WaitingReasons: []string{"CrashLoopBackOff"}},
		},
		logs: Logs{Pod: "broken-api-5d8c", Preview: "fatal config missing"},
	}, 80)

	response, err := service.AskWithContext(
		context.Background(),
		"show logs for it",
		Context{Pods: []string{"broken-api-5d8c"}, LastIntent: "unhealthy_pods"},
	)
	if err != nil {
		t.Fatalf("AskWithContext returned error: %v", err)
	}

	if response.Intent != "pod_details" {
		t.Fatalf("intent = %q, want pod_details", response.Intent)
	}
	if !strings.Contains(response.Answer, "broken-api-5d8c") || !strings.Contains(response.Answer, "fatal config missing") {
		t.Fatalf("answer %q did not include context pod details", response.Answer)
	}
}

func TestServiceAskUsesCrashLoopPodFactForItFollowup(t *testing.T) {
	service := NewService(fakeQuerier{
		results: map[string]json.RawMessage{
			`sum by (pod) (kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})`: vectorPayload(
				sample(map[string]string{"pod": "broken-api-5d8c"}, "1"),
			),
		},
	}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{
			{Name: "broken-api-5d8c", Phase: "Running", RestartCount: 3, WaitingReasons: []string{"CrashLoopBackOff"}},
		},
		logs: Logs{Pod: "broken-api-5d8c", Preview: "fatal config missing"},
	}, 80)

	first, err := service.Ask(context.Background(), "any pod in crash back loop?")
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}
	contextPods := podsFromFacts(first.Facts)
	if len(contextPods) != 1 || contextPods[0] != "broken-api-5d8c" {
		t.Fatalf("context pods = %#v, want crashloop pod", contextPods)
	}

	second, err := service.AskWithContext(
		context.Background(),
		"tell me more about it",
		Context{Pods: contextPods, LastIntent: first.Intent},
	)
	if err != nil {
		t.Fatalf("AskWithContext returned error: %v", err)
	}
	if !strings.Contains(second.Answer, "broken-api-5d8c") || !strings.Contains(second.Answer, "fatal config missing") {
		t.Fatalf("answer %q did not include context pod details", second.Answer)
	}
}

func TestServiceAskUsesContextOrdinalFollowup(t *testing.T) {
	service := NewService(fakeQuerier{}, "monitoring-tool").WithKubernetes(fakeKubernetes{
		pods: []Pod{
			{Name: "first-api-1111", Phase: "Running"},
			{Name: "second-api-2222", Phase: "Pending"},
		},
	}, 80)

	response, err := service.AskWithContext(
		context.Background(),
		"tell me more about the second one",
		Context{Pods: []string{"first-api-1111", "second-api-2222"}, LastIntent: "unhealthy_pods"},
	)
	if err != nil {
		t.Fatalf("AskWithContext returned error: %v", err)
	}

	if !strings.Contains(response.Answer, "second-api-2222") {
		t.Fatalf("answer %q did not include second context pod", response.Answer)
	}
	if strings.Contains(response.Answer, "first-api-1111") {
		t.Fatalf("answer %q included first context pod", response.Answer)
	}
}

type vectorSample struct {
	metric map[string]string
	value  string
}

func sample(metric map[string]string, value string) vectorSample {
	return vectorSample{metric: metric, value: value}
}

func vectorPayload(samples ...vectorSample) json.RawMessage {
	parts := make([]string, 0, len(samples))
	for _, item := range samples {
		metric, _ := json.Marshal(item.metric)
		parts = append(parts, fmt.Sprintf(`{"metric":%s,"value":[123,%q]}`, metric, item.value))
	}
	return json.RawMessage(fmt.Sprintf(`{"resultType":"vector","result":[%s]}`, strings.Join(parts, ",")))
}

func hasFact(facts []Fact, label string, value string) bool {
	for _, fact := range facts {
		if fact.Label == label && fact.Value == value {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func podsFromFacts(facts []Fact) []string {
	pods := []string{}
	for _, fact := range facts {
		if fact.Label == "Pod" {
			pods = append(pods, fact.Value)
		}
	}
	return pods
}
