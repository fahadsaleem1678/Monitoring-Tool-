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

type Service struct {
	prometheus PrometheusQuerier
	namespace  string
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
	return &Service{prometheus: prometheus, namespace: namespace}
}

func (s *Service) Ask(ctx context.Context, message string) (Response, error) {
	normalized, err := normalizeMessage(message)
	if err != nil {
		return Response{}, err
	}

	intent := matchIntent(normalized)
	if intent == nil {
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
		keywords:    []string{"restarts", "restarting", "restarted"},
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
