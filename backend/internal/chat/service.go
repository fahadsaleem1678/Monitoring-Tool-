package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	IntentUnsupported = "unsupported"
	ConfidenceHigh    = "high"
	ConfidenceLow     = "low"
	defaultNamespace  = "monitoring-tool"
	maxMessageLength  = 500
)

type PrometheusQuerier interface {
	InstantQuery(ctx context.Context, query string) (json.RawMessage, error)
}

type KubernetesReader interface {
	Pods(ctx context.Context, namespace string) ([]Pod, error)
	Events(ctx context.Context, namespace string) ([]Event, error)
	Logs(ctx context.Context, namespace, pod string, tailLines int) (Logs, error)
}

type Service struct {
	prometheus  PrometheusQuerier
	kubernetes  KubernetesReader
	namespace   string
	maxLogLines int
}

type Pod struct {
	Namespace      string
	Name           string
	Phase          string
	RestartCount   int
	WaitingReasons []string
}

type Event struct {
	Namespace     string
	Name          string
	Reason        string
	Message       string
	Type          string
	LastTimestamp string
}

type Logs struct {
	Namespace string
	Pod       string
	Preview   string
}

type Response struct {
	Answer      string   `json:"answer"`
	Intent      string   `json:"intent"`
	Confidence  string   `json:"confidence"`
	Facts       []Fact   `json:"facts"`
	Queries     []string `json:"queries"`
	Suggestions []string `json:"suggestions"`
}

type Fact struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Severity string `json:"severity"`
}

type intentDefinition struct {
	id                  string
	label               string
	query               func(namespace string) string
	keywords            []string
	seriesLabel         string
	healthyWhenPositive bool
	healthyText         string
	warningText         string
}

func NewService(prometheus PrometheusQuerier, namespace string) *Service {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = defaultNamespace
	}
	return &Service{prometheus: prometheus, namespace: namespace, maxLogLines: 80}
}

func (s *Service) WithKubernetes(kubernetes KubernetesReader, maxLogLines int) *Service {
	s.kubernetes = kubernetes
	if maxLogLines > 0 {
		s.maxLogLines = maxLogLines
	}
	return s
}

func (s *Service) Ask(ctx context.Context, message string) (Response, error) {
	normalized, err := normalizeMessage(message)
	if err != nil {
		return Response{}, err
	}

	intent := matchIntent(normalized)
	if intent == nil {
		if isUnhealthyPodListQuestion(normalized) {
			return s.answerUnhealthyPods(ctx)
		}
		if isPodDetailQuestion(normalized) {
			return s.answerPodDetail(ctx, message, normalized)
		}
		return unsupportedResponse(), nil
	}

	query := intent.query(s.namespace)
	raw, err := s.prometheus.InstantQuery(ctx, query)
	if err != nil {
		return Response{}, fmt.Errorf("run %s check: %w", intent.id, err)
	}

	result, err := readVector(raw)
	if err != nil {
		return Response{}, fmt.Errorf("read %s check: %w", intent.id, err)
	}

	count := int(math.Round(result.total))
	names := result.nonzeroLabels(intent.seriesLabel)
	severity, answer := formatIntentAnswer(intent, count, s.namespace, names)

	return Response{
		Answer:     answer,
		Intent:     intent.id,
		Confidence: ConfidenceHigh,
		Facts: []Fact{
			{
				Label:    intent.label,
				Value:    fmt.Sprintf("%d", count),
				Severity: severity,
			},
		},
		Queries:     []string{query},
		Suggestions: suggestionsFor(intent.id),
	}, nil
}

func formatIntentAnswer(intent *intentDefinition, count int, namespace string, names []string) (string, string) {
	if intent.healthyWhenPositive {
		if count > 0 {
			return "healthy", fmt.Sprintf(intent.healthyText, count)
		}
		return "warning", fmt.Sprintf(intent.warningText, namespace)
	}

	if count > 0 {
		answer := fmt.Sprintf(intent.warningText, count, namespace)
		if len(names) > 0 {
			answer = fmt.Sprintf("%s Affected pods: %s.", answer, strings.Join(names, ", "))
		}
		return "warning", answer
	}
	return "healthy", fmt.Sprintf(intent.healthyText, namespace)
}

func normalizeMessage(message string) (string, error) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "", fmt.Errorf("message is required")
	}
	if len(trimmed) > maxMessageLength {
		return "", fmt.Errorf("message cannot exceed %d characters", maxMessageLength)
	}

	replacer := strings.NewReplacer(
		"?", " ",
		"!", " ",
		".", " ",
		",", " ",
		"-", " ",
		"_", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(strings.ToLower(trimmed))), " "), nil
}

func matchIntent(message string) *intentDefinition {
	for _, intent := range intents {
		for _, keyword := range intent.keywords {
			if strings.Contains(message, keyword) {
				if intent.id == "pod_restarts" && isMutationRestartRequest(message) {
					continue
				}
				return &intent
			}
		}
	}
	return nil
}

func unsupportedResponse() Response {
	return Response{
		Answer:      "I can only answer a focused set of read-only cluster health questions right now.",
		Intent:      IntentUnsupported,
		Confidence:  ConfidenceLow,
		Facts:       []Fact{},
		Queries:     []string{},
		Suggestions: defaultSuggestions(),
	}
}

type vectorResult struct {
	total  float64
	series []vectorSeries
}

type vectorSeries struct {
	metric map[string]string
	value  float64
}

func (r vectorResult) nonzeroLabels(label string) []string {
	if label == "" {
		return nil
	}
	seen := map[string]bool{}
	names := []string{}
	for _, series := range r.series {
		name := strings.TrimSpace(series.metric[label])
		if name == "" || seen[name] || series.value <= 0 {
			continue
		}
		seen[name] = true
		names = append(names, name)
		if len(names) == 8 {
			break
		}
	}
	return names
}

func readVector(raw json.RawMessage) (vectorResult, error) {
	var body struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return vectorResult{}, err
	}
	if body.ResultType != "vector" {
		return vectorResult{}, fmt.Errorf("expected vector result, got %q", body.ResultType)
	}

	result := vectorResult{series: []vectorSeries{}}
	for _, item := range body.Result {
		if len(item.Value) < 2 {
			continue
		}
		text, ok := item.Value[1].(string)
		if !ok {
			return vectorResult{}, fmt.Errorf("vector value was not a string")
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return vectorResult{}, fmt.Errorf("vector value %q was not numeric", text)
		}
		result.total += value
		result.series = append(result.series, vectorSeries{metric: item.Metric, value: value})
	}
	return result, nil
}

func defaultSuggestions() []string {
	return []string{
		"Are my pods healthy?",
		"Any crash loops?",
		"Which pods are restarting?",
		"Are nodes ready?",
	}
}

func suggestionsFor(intent string) []string {
	switch intent {
	case "pod_crashloops":
		return []string{"Which pods are restarting?", "Any image pull errors?"}
	case "pod_restarts":
		return []string{"Any crash loops?", "Are my pods healthy?"}
	case "node_ready":
		return []string{"How many pods are running?", "Any pending pods?"}
	default:
		return defaultSuggestions()
	}
}

func isPodDetailQuestion(message string) bool {
	return strings.Contains(message, "why") ||
		strings.Contains(message, "detail") ||
		strings.Contains(message, "failing") ||
		strings.Contains(message, "logs") ||
		strings.Contains(message, "events") ||
		strings.Contains(message, "pod named") ||
		strings.Contains(message, "named") ||
		strings.Contains(message, "exists")
}

func isUnhealthyPodListQuestion(message string) bool {
	hasPod := strings.Contains(message, "pod")
	hasListIntent := strings.Contains(message, "which") ||
		strings.Contains(message, "list") ||
		strings.Contains(message, "show") ||
		strings.Contains(message, "tell me") ||
		strings.Contains(message, "what")
	hasUnhealthy := strings.Contains(message, "failing") ||
		strings.Contains(message, "failed") ||
		strings.Contains(message, "unhealthy") ||
		strings.Contains(message, "bad") ||
		strings.Contains(message, "problem") ||
		strings.Contains(message, "broken") ||
		strings.Contains(message, "not healthy")
	return hasPod && hasListIntent && hasUnhealthy
}

func isMutationRestartRequest(message string) bool {
	if !strings.Contains(message, "restart") {
		return false
	}
	return strings.Contains(message, "deployment") ||
		strings.Contains(message, "deploy") ||
		strings.Contains(message, "service") ||
		strings.Contains(message, "rollout") ||
		strings.Contains(message, "please restart") ||
		strings.Contains(message, "can you restart")
}

func (s *Service) answerUnhealthyPods(ctx context.Context) (Response, error) {
	if s.kubernetes == nil {
		return Response{
			Answer:      "I can count unhealthy pods, but Kubernetes pod listing is not configured for this backend yet.",
			Intent:      IntentUnsupported,
			Confidence:  ConfidenceLow,
			Facts:       []Fact{},
			Queries:     []string{},
			Suggestions: defaultSuggestions(),
		}, nil
	}

	pods, err := s.kubernetes.Pods(ctx, s.namespace)
	if err != nil {
		return Response{}, fmt.Errorf("list pods from kubernetes mcp: %w", err)
	}
	unhealthy := unhealthyPods(pods)
	if len(unhealthy) == 0 {
		return Response{
			Answer:      fmt.Sprintf("Pods look healthy in %s. I did not find pending, failed, unknown, waiting, or restarting pods.", s.namespace),
			Intent:      "unhealthy_pods",
			Confidence:  ConfidenceHigh,
			Facts:       []Fact{{Label: "Unhealthy pods", Value: "0", Severity: "healthy"}},
			Queries:     []string{"kubernetes.pods"},
			Suggestions: []string{"Any crash loops?", "Any image pull errors?", "Are nodes ready?"},
		}, nil
	}

	names := make([]string, 0, len(unhealthy))
	facts := []Fact{{Label: "Unhealthy pods", Value: fmt.Sprintf("%d", len(unhealthy)), Severity: "warning"}}
	for _, pod := range unhealthy {
		names = append(names, describePodShort(pod))
		facts = append(facts, podFacts(pod)...)
	}

	return Response{
		Answer:      fmt.Sprintf("I found %d unhealthy pod(s) in %s: %s.", len(unhealthy), s.namespace, strings.Join(names, "; ")),
		Intent:      "unhealthy_pods",
		Confidence:  ConfidenceHigh,
		Facts:       limitFacts(facts, 18),
		Queries:     []string{"kubernetes.pods"},
		Suggestions: podNameSuggestions(unhealthy),
	}, nil
}

func (s *Service) answerPodDetail(ctx context.Context, originalMessage, normalizedMessage string) (Response, error) {
	if s.kubernetes == nil {
		return Response{
			Answer:      "I can identify cluster health issues, but Kubernetes pod details are not configured for this backend yet.",
			Intent:      IntentUnsupported,
			Confidence:  ConfidenceLow,
			Facts:       []Fact{},
			Queries:     []string{},
			Suggestions: []string{"Any crash loops?", "Any pending pods?", "Any image pull errors?"},
		}, nil
	}

	pods, err := s.kubernetes.Pods(ctx, s.namespace)
	if err != nil {
		return Response{}, fmt.Errorf("list pods from kubernetes mcp: %w", err)
	}
	pod, ok := matchPodFromMessage(originalMessage, normalizedMessage, pods)
	if !ok {
		answer := "I could not match that question to a pod name in the monitoring-tool namespace."
		if len(pods) > 0 {
			answer = fmt.Sprintf("%s I can see pods such as: %s.", answer, strings.Join(firstPodNames(pods, 6), ", "))
		}
		return Response{
			Answer:      answer,
			Intent:      "pod_details",
			Confidence:  ConfidenceLow,
			Facts:       []Fact{},
			Queries:     []string{"kubernetes.pods"},
			Suggestions: podNameSuggestions(pods),
		}, nil
	}

	events, eventsErr := s.kubernetes.Events(ctx, s.namespace)
	logs, logsErr := s.kubernetes.Logs(ctx, s.namespace, pod.Name, s.maxLogLines)

	answerParts := []string{fmt.Sprintf("%s is %s", pod.Name, emptyFallback(pod.Phase, "unknown phase"))}
	if pod.RestartCount > 0 {
		answerParts = append(answerParts, fmt.Sprintf("restart count is %d", pod.RestartCount))
	}
	if len(pod.WaitingReasons) > 0 {
		answerParts = append(answerParts, fmt.Sprintf("waiting reason: %s", strings.Join(pod.WaitingReasons, ", ")))
	}
	if event := mostRecentPodEvent(pod.Name, events); event != nil {
		answerParts = append(answerParts, fmt.Sprintf("latest event: %s - %s", emptyFallback(event.Reason, "event"), event.Message))
	} else if eventsErr != nil {
		answerParts = append(answerParts, "events were not available")
	}
	if strings.TrimSpace(logs.Preview) != "" {
		answerParts = append(answerParts, "recent logs: "+singleLinePreview(logs.Preview, 260))
	} else if logsErr != nil {
		answerParts = append(answerParts, "logs were not available")
	}

	return Response{
		Answer:      strings.Join(answerParts, ". ") + ".",
		Intent:      "pod_details",
		Confidence:  ConfidenceHigh,
		Facts:       podFacts(pod),
		Queries:     []string{"kubernetes.pods", "kubernetes.events", "kubernetes.logs"},
		Suggestions: []string{"Any crash loops?", "Any image pull errors?", "Which pods are restarting?"},
	}, nil
}

func unhealthyPods(pods []Pod) []Pod {
	result := []Pod{}
	for _, pod := range pods {
		if pod.Name == "" {
			continue
		}
		if pod.Phase != "Running" || pod.RestartCount > 0 || len(pod.WaitingReasons) > 0 {
			result = append(result, pod)
		}
	}
	return result
}

func describePodShort(pod Pod) string {
	parts := []string{pod.Name}
	if pod.Phase != "" {
		parts = append(parts, "phase "+pod.Phase)
	}
	if pod.RestartCount > 0 {
		parts = append(parts, fmt.Sprintf("%d restart(s)", pod.RestartCount))
	}
	if len(pod.WaitingReasons) > 0 {
		parts = append(parts, strings.Join(pod.WaitingReasons, ", "))
	}
	return strings.Join(parts, ", ")
}

func limitFacts(facts []Fact, limit int) []Fact {
	if limit <= 0 || len(facts) <= limit {
		return facts
	}
	return facts[:limit]
}

func matchPodFromMessage(originalMessage, normalizedMessage string, pods []Pod) (Pod, bool) {
	compactOriginal := compactName(originalMessage)
	compactNormalized := compactName(normalizedMessage)
	best := Pod{}
	bestScore := 0
	for _, pod := range pods {
		compactPod := compactName(pod.Name)
		if compactPod == "" {
			continue
		}
		score := 0
		if strings.Contains(compactOriginal, compactPod) || strings.Contains(compactNormalized, compactPod) {
			score = len(compactPod)
		} else {
			for _, token := range strings.Fields(normalizedMessage) {
				compactToken := compactName(token)
				if len(compactToken) >= 4 && strings.Contains(compactPod, compactToken) {
					score = len(compactToken)
				}
			}
		}
		if score > bestScore {
			best = pod
			bestScore = score
		}
	}
	return best, bestScore > 0
}

func compactName(value string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(value) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func mostRecentPodEvent(podName string, events []Event) *Event {
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		if strings.Contains(event.Name, podName) || strings.Contains(event.Message, podName) {
			return &event
		}
	}
	return nil
}

func podFacts(pod Pod) []Fact {
	facts := []Fact{
		{Label: "Pod", Value: pod.Name, Severity: "healthy"},
		{Label: "Phase", Value: emptyFallback(pod.Phase, "unknown"), Severity: severityForPod(pod)},
		{Label: "Restarts", Value: fmt.Sprintf("%d", pod.RestartCount), Severity: severityForRestarts(pod.RestartCount)},
	}
	if len(pod.WaitingReasons) > 0 {
		facts = append(facts, Fact{Label: "Waiting reason", Value: strings.Join(pod.WaitingReasons, ", "), Severity: "warning"})
	}
	return facts
}

func podNameSuggestions(pods []Pod) []string {
	suggestions := []string{}
	for _, pod := range pods {
		if pod.Name == "" {
			continue
		}
		suggestions = append(suggestions, "Show details for "+pod.Name)
		if len(suggestions) == 4 {
			break
		}
	}
	if len(suggestions) == 0 {
		return defaultSuggestions()
	}
	return suggestions
}

func firstPodNames(pods []Pod, limit int) []string {
	names := []string{}
	for _, pod := range pods {
		if strings.TrimSpace(pod.Name) == "" {
			continue
		}
		names = append(names, pod.Name)
		if len(names) == limit {
			break
		}
	}
	return names
}

func severityForPod(pod Pod) string {
	if pod.Phase != "Running" || len(pod.WaitingReasons) > 0 {
		return "warning"
	}
	return "healthy"
}

func severityForRestarts(count int) string {
	if count > 0 {
		return "warning"
	}
	return "healthy"
}

func emptyFallback(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func singleLinePreview(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit > 0 && len(value) > limit {
		return value[:limit] + "..."
	}
	return value
}

var intents = []intentDefinition{
	{
		id:    "pod_crashloops",
		label: "CrashLoopBackOff pods",
		query: func(namespace string) string {
			return fmt.Sprintf(`sum by (pod) (kube_pod_container_status_waiting_reason{namespace=%q,reason="CrashLoopBackOff"})`, namespace)
		},
		seriesLabel: "pod",
		keywords:    []string{"crash loop", "crashloop", "crashloopbackoff"},
		healthyText: "No pods are showing CrashLoopBackOff in %s.",
		warningText: "There are %d pods showing CrashLoopBackOff in %s.",
	},
	{
		id:    "pod_image_pull_errors",
		label: "Image pull error pods",
		query: func(namespace string) string {
			return fmt.Sprintf(`sum by (pod) (kube_pod_container_status_waiting_reason{namespace=%q,reason=~"ImagePullBackOff|ErrImagePull"})`, namespace)
		},
		seriesLabel: "pod",
		keywords:    []string{"image pull", "imagepull", "errimagepull", "pull error", "bad image"},
		healthyText: "No pods are reporting image pull errors in %s.",
		warningText: "There are %d pods with image pull errors in %s.",
	},
	{
		id:    "pod_pending",
		label: "Pending pods",
		query: func(namespace string) string {
			return fmt.Sprintf(`sum by (pod) (kube_pod_status_phase{namespace=%q,phase="Pending"})`, namespace)
		},
		seriesLabel: "pod",
		keywords:    []string{"pending", "unschedulable", "stuck scheduling"},
		healthyText: "No pods are pending in %s.",
		warningText: "There are %d pending pods in %s.",
	},
	{
		id:    "pod_restarts",
		label: "Pod restarts in the last 5 minutes",
		query: func(namespace string) string {
			return fmt.Sprintf(`sum by (pod) (increase(kube_pod_container_status_restarts_total{namespace=%q}[5m]))`, namespace)
		},
		seriesLabel: "pod",
		keywords:    []string{"restart", "restarts", "restarting", "restarted"},
		healthyText: "No pod restarts were reported in %s during the last 5 minutes.",
		warningText: "There were %d pod restarts in %s during the last 5 minutes.",
	},
	{
		id:    "node_ready",
		label: "Ready nodes",
		query: func(_ string) string {
			return `sum(kube_node_status_condition{condition="Ready",status="true"})`
		},
		keywords:            []string{"node ready", "nodes ready", "ready nodes", "node health", "nodes healthy"},
		healthyWhenPositive: true,
		healthyText:         "%d nodes are ready in the cluster.",
		warningText:         "No ready nodes were reported for %s.",
	},
	{
		id:    "pod_running_count",
		label: "Running pods",
		query: func(namespace string) string {
			return fmt.Sprintf(`sum(kube_pod_status_phase{namespace=%q,phase="Running"})`, namespace)
		},
		keywords:            []string{"running pod", "pods running", "how many pods", "pod count"},
		healthyWhenPositive: true,
		healthyText:         "%d pods are running.",
		warningText:         "No running pods were reported in %s.",
	},
	{
		id:    "scrape_targets",
		label: "Prometheus targets up",
		query: func(_ string) string {
			return `sum(up)`
		},
		keywords:            []string{"targets", "scrape", "prometheus up", "metrics working"},
		healthyWhenPositive: true,
		healthyText:         "%d Prometheus scrape targets are up.",
		warningText:         "No healthy Prometheus scrape targets were reported for %s.",
	},
	{
		id:    "pod_health",
		label: "Non-running pods",
		query: func(namespace string) string {
			return fmt.Sprintf(`sum by (pod) (kube_pod_status_phase{namespace=%q,phase=~"Pending|Failed|Unknown"})`, namespace)
		},
		seriesLabel: "pod",
		keywords:    []string{"pod health", "pods healthy", "pods ok", "pods okay", "are my pods healthy"},
		healthyText: "Pods look healthy in %s. No pending, failed, or unknown pods were reported.",
		warningText: "There are %d pods in %s that are pending, failed, or unknown.",
	},
}
