package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	IntentUnsupported = "unsupported"
	ConfidenceHigh   = "high"
	ConfidenceLow    = "low"
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

	value, err := firstVectorValue(raw)
	if err != nil {
		return Response{}, fmt.Errorf("read %s check: %w", intent.id, err)
	}

	count := int(math.Round(value))
	severity, answer := formatIntentAnswer(intent, count, s.namespace)

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

func formatIntentAnswer(intent *intentDefinition, count int, namespace string) (string, string) {
	if intent.healthyWhenPositive {
		if count > 0 {
			return "healthy", fmt.Sprintf(intent.healthyText, count)
		}
		return "warning", fmt.Sprintf(intent.warningText, namespace)
	}

	if count > 0 {
		return "warning", fmt.Sprintf(intent.warningText, count, namespace)
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

func firstVectorValue(raw json.RawMessage) (float64, error) {
	var body struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value []any `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return 0, err
	}
	if body.ResultType != "vector" {
		return 0, fmt.Errorf("expected vector result, got %q", body.ResultType)
	}
	if len(body.Result) == 0 || len(body.Result[0].Value) < 2 {
		return 0, nil
	}
	text, ok := body.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("vector value was not a string")
	}
	var value float64
	if _, err := fmt.Sscanf(text, "%f", &value); err != nil {
		return 0, fmt.Errorf("vector value %q was not numeric", text)
	}
	return value, nil
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
			return fmt.Sprintf(`sum(kube_pod_container_status_waiting_reason{namespace=%q,reason="CrashLoopBackOff"})`, namespace)
		},
		keywords:    []string{"crash loop", "crashloop", "crashloopbackoff"},
		healthyText: "No pods are showing CrashLoopBackOff in %s.",
		warningText: "There are %d pods showing CrashLoopBackOff in %s.",
	},
	{
		id:    "pod_image_pull_errors",
		label: "Image pull error pods",
		query: func(namespace string) string {
			return fmt.Sprintf(`sum(kube_pod_container_status_waiting_reason{namespace=%q,reason=~"ImagePullBackOff|ErrImagePull"})`, namespace)
		},
		keywords:    []string{"image pull", "imagepull", "errimagepull", "pull error", "bad image"},
		healthyText: "No pods are reporting image pull errors in %s.",
		warningText: "There are %d pods with image pull errors in %s.",
	},
	{
		id:    "pod_pending",
		label: "Pending pods",
		query: func(namespace string) string {
			return fmt.Sprintf(`sum(kube_pod_status_phase{namespace=%q,phase="Pending"})`, namespace)
		},
		keywords:    []string{"pending", "unschedulable", "stuck scheduling"},
		healthyText: "No pods are pending in %s.",
		warningText: "There are %d pending pods in %s.",
	},
	{
		id:    "pod_restarts",
		label: "Pod restarts in the last 5 minutes",
		query: func(namespace string) string {
			return fmt.Sprintf(`sum(increase(kube_pod_container_status_restarts_total{namespace=%q}[5m]))`, namespace)
		},
		keywords:    []string{"restart", "restarting", "restarted"},
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
			return fmt.Sprintf(`sum(kube_pod_status_phase{namespace=%q,phase=~"Pending|Failed|Unknown"})`, namespace)
		},
		keywords:    []string{"pod health", "pods healthy", "pods ok", "pods okay", "are my pods healthy"},
		healthyText: "Pods look healthy in %s. No pending, failed, or unknown pods were reported.",
		warningText: "There are %d pods in %s that are pending, failed, or unknown.",
	},
}
