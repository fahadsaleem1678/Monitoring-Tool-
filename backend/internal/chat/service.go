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
	Namespaces(ctx context.Context) ([]Namespace, error)
	Pods(ctx context.Context, namespace string) ([]Pod, error)
	Events(ctx context.Context, namespace string) ([]Event, error)
	Logs(ctx context.Context, namespace, pod string, tailLines int) (Logs, error)
}

type IntentRouter interface {
	Route(ctx context.Context, message string, chatContext Context, intents []string) (string, error)
}

type GeneralAnswerer interface {
	AnswerGeneral(ctx context.Context, message string, chatContext Context) (string, error)
}

type Service struct {
	prometheus      PrometheusQuerier
	kubernetes      KubernetesReader
	router          IntentRouter
	generalAnswerer GeneralAnswerer
	namespace       string
	maxLogLines     int
}

type Pod struct {
	Namespace      string
	Name           string
	Phase          string
	RestartCount   int
	WaitingReasons []string
}

type Namespace struct {
	Name  string
	Phase string
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
	Engine      string   `json:"engine"`
	Facts       []Fact   `json:"facts"`
	Queries     []string `json:"queries"`
	Suggestions []string `json:"suggestions"`
}

type Context struct {
	Pods       []string `json:"pods"`
	LastIntent string   `json:"last_intent"`
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

func (s *Service) WithIntentRouter(router IntentRouter) *Service {
	s.router = router
	return s
}

func (s *Service) WithGeneralAnswerer(answerer GeneralAnswerer) *Service {
	s.generalAnswerer = answerer
	return s
}

func (s *Service) Ask(ctx context.Context, message string) (Response, error) {
	return s.AskWithContext(ctx, message, Context{})
}

func (s *Service) AskWithContext(ctx context.Context, message string, chatContext Context) (Response, error) {
	normalized, err := normalizeMessage(message)
	if err != nil {
		return Response{}, err
	}

	intent := matchIntent(normalized)
	if intent == nil {
		if isPrioritySummaryQuestion(normalized) {
			return s.answerPrioritySummary(ctx)
		}
		if isNamespaceListQuestion(normalized) {
			return s.answerNamespaces(ctx)
		}
		if isAllPodListQuestion(normalized) {
			return s.answerAllPods(ctx)
		}
		if isUnhealthyPodListQuestion(normalized) {
			return s.answerUnhealthyPods(ctx)
		}
		if isPodDetailQuestion(normalized) || isFollowUpPodQuestion(normalized) {
			return s.answerPodDetail(ctx, message, normalized, chatContext)
		}
		if s.router != nil {
			routedIntent, err := s.router.Route(ctx, message, chatContext, routableIntentIDs())
			if err != nil {
				response := unsupportedResponse()
				response.Answer = "I could not use the optional LLM router right now, and this did not match a deterministic cluster check."
				response.Engine = "llm-unavailable"
				return response, nil
			}
			if routedIntent != "" && routedIntent != IntentUnsupported {
				response, ok, err := s.answerRoutedIntent(ctx, routedIntent, message, normalized, chatContext)
				if err != nil {
					return Response{}, err
				}
				if ok {
					response.Engine = "llm-routed"
					return response, nil
				}
			}
		}
		if s.generalAnswerer != nil && isGeneralKnowledgeQuestion(normalized) && !isMutationRequest(normalized) {
			answer, err := s.generalAnswerer.AnswerGeneral(ctx, message, chatContext)
			if err != nil {
				response := unsupportedResponse()
				response.Answer = "I could not use the optional LLM for a general answer right now. Try a supported cluster health question."
				response.Engine = "llm-unavailable"
				return response, nil
			}
			return Response{
				Answer:      answer,
				Intent:      "general_question",
				Confidence:  ConfidenceLow,
				Engine:      "llm-general",
				Facts:       []Fact{},
				Queries:     []string{},
				Suggestions: defaultSuggestions(),
			}, nil
		}
		return unsupportedResponse(), nil
	}

	return s.answerMetricIntent(ctx, intent, s.deterministicEngine())
}

func (s *Service) deterministicEngine() string {
	if s.router != nil || s.generalAnswerer != nil {
		return "deterministic+llm-router"
	}
	return "deterministic"
}

func (s *Service) answerMetricIntent(ctx context.Context, intent *intentDefinition, engine string) (Response, error) {
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
	facts := []Fact{
		{
			Label:    intent.label,
			Value:    fmt.Sprintf("%d", count),
			Severity: severity,
		},
	}
	for _, name := range names {
		facts = append(facts, Fact{Label: "Pod", Value: name, Severity: severity})
	}

	return Response{
		Answer:      answer,
		Intent:      intent.id,
		Confidence:  ConfidenceHigh,
		Engine:      engine,
		Facts:       facts,
		Queries:     []string{query},
		Suggestions: suggestionsForIntentAndPods(intent.id, names),
	}, nil
}

func (s *Service) answerRoutedIntent(ctx context.Context, intentID, message, normalized string, chatContext Context) (Response, bool, error) {
	if intent := intentByID(intentID); intent != nil {
		response, err := s.answerMetricIntent(ctx, intent, "llm-routed")
		return response, true, err
	}
	switch intentID {
	case "cluster_priority_summary":
		response, err := s.answerPrioritySummary(ctx)
		return response, true, err
	case "namespaces":
		response, err := s.answerNamespaces(ctx)
		return response, true, err
	case "all_pods":
		response, err := s.answerAllPods(ctx)
		return response, true, err
	case "unhealthy_pods":
		response, err := s.answerUnhealthyPods(ctx)
		return response, true, err
	case "pod_details":
		response, err := s.answerPodDetail(ctx, message, normalized, chatContext)
		return response, true, err
	default:
		return Response{}, false, nil
	}
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

func intentByID(intentID string) *intentDefinition {
	for _, intent := range intents {
		if intent.id == intentID {
			return &intent
		}
	}
	return nil
}

func routableIntentIDs() []string {
	ids := make([]string, 0, len(intents)+6)
	for _, intent := range intents {
		ids = append(ids, intent.id)
	}
	return append(ids, "cluster_priority_summary", "namespaces", "all_pods", "unhealthy_pods", "pod_details", IntentUnsupported)
}

func unsupportedResponse() Response {
	return Response{
		Answer:      "I can only answer a focused set of read-only cluster health questions right now.",
		Intent:      IntentUnsupported,
		Confidence:  ConfidenceLow,
		Engine:      "deterministic",
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
		"Any image pull errors?",
		"Any pending pods?",
		"Are nodes ready?",
		"How many pods are running?",
		"Are Prometheus targets up?",
		"What should I fix first?",
		"Show unhealthy pods",
		"List all pods",
	}
}

func suggestionsFor(intent string) []string {
	return suggestionsForIntentAndPods(intent, nil)
}

func suggestionsForIntentAndPods(intent string, pods []string) []string {
	if len(pods) > 0 {
		suggestions := make([]string, 0, len(pods)+2)
		for _, pod := range pods {
			suggestions = append(suggestions, "Show details for "+pod)
			if len(suggestions) == 4 {
				break
			}
		}
		suggestions = append(suggestions, "Show logs for it")
		return suggestions
	}
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

func isFollowUpPodQuestion(message string) bool {
	return strings.Contains(message, " it") ||
		message == "it" ||
		strings.Contains(message, "that pod") ||
		strings.Contains(message, "this pod") ||
		strings.Contains(message, "first one") ||
		strings.Contains(message, "second one") ||
		strings.Contains(message, "third one")
}

func isPrioritySummaryQuestion(message string) bool {
	return strings.Contains(message, "fix first") ||
		strings.Contains(message, "most serious") ||
		strings.Contains(message, "highest priority") ||
		strings.Contains(message, "priority") ||
		strings.Contains(message, "summarize cluster") ||
		strings.Contains(message, "cluster problems") ||
		strings.Contains(message, "health report") ||
		strings.Contains(message, "quick report")
}

func isNamespaceListQuestion(message string) bool {
	hasNamespace := strings.Contains(message, "namespace") || strings.Contains(message, "namespaces")
	hasListIntent := strings.Contains(message, "list") ||
		strings.Contains(message, "show") ||
		strings.Contains(message, "tell me") ||
		strings.Contains(message, "what") ||
		strings.Contains(message, "all") ||
		strings.Contains(message, "names")
	return hasNamespace && hasListIntent
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

func isAllPodListQuestion(message string) bool {
	hasPod := strings.Contains(message, "pod")
	hasListIntent := strings.Contains(message, "list") ||
		strings.Contains(message, "show") ||
		strings.Contains(message, "all") ||
		strings.Contains(message, "names")
	hasSpecificHealthIntent := strings.Contains(message, "unhealthy") ||
		strings.Contains(message, "failing") ||
		strings.Contains(message, "failed") ||
		strings.Contains(message, "crash") ||
		strings.Contains(message, "restart") ||
		strings.Contains(message, "pending") ||
		strings.Contains(message, "image")
	return hasPod && hasListIntent && !hasSpecificHealthIntent
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

func isMutationRequest(message string) bool {
	mutationWords := []string{
		"delete", "remove", "restart", "scale", "patch", "apply", "create", "exec", "rollback", "rollout", "upgrade",
	}
	for _, word := range mutationWords {
		if strings.Contains(message, word) {
			return true
		}
	}
	return false
}

func isGeneralKnowledgeQuestion(message string) bool {
	if strings.Contains(message, "what is") ||
		strings.Contains(message, "what are") ||
		strings.Contains(message, "how does") ||
		strings.Contains(message, "how do") ||
		strings.Contains(message, "why does") ||
		strings.Contains(message, "why do") ||
		strings.Contains(message, "explain") ||
		strings.Contains(message, "difference between") ||
		strings.Contains(message, "meaning of") ||
		strings.Contains(message, "define") {
		return true
	}
	return false
}

func (s *Service) answerPrioritySummary(ctx context.Context) (Response, error) {
	if s.kubernetes == nil {
		return Response{
			Answer:      "I can summarize basic metrics, but Kubernetes pod details are not configured for this backend yet.",
			Intent:      IntentUnsupported,
			Confidence:  ConfidenceLow,
			Engine:      s.deterministicEngine(),
			Facts:       []Fact{},
			Queries:     []string{},
			Suggestions: defaultSuggestions(),
		}, nil
	}

	pods, err := s.kubernetes.Pods(ctx, s.namespace)
	if err != nil {
		return s.answerPrioritySummaryFromMetrics(ctx)
	}
	problems := rankedPodProblems(pods)
	if len(problems) == 0 {
		return Response{
			Answer:      fmt.Sprintf("I do not see urgent pod problems in %s. Pods are running without waiting reasons or restarts.", s.namespace),
			Intent:      "cluster_priority_summary",
			Confidence:  ConfidenceHigh,
			Engine:      s.deterministicEngine(),
			Facts:       []Fact{{Label: "Priority issues", Value: "0", Severity: "healthy"}},
			Queries:     []string{"kubernetes.pods"},
			Suggestions: []string{"List all pods", "Are nodes ready?", "Any image pull errors?"},
		}, nil
	}

	if len(problems) > 5 {
		problems = problems[:5]
	}
	lines := make([]string, 0, len(problems))
	facts := []Fact{{Label: "Priority issues", Value: fmt.Sprintf("%d", len(problems)), Severity: "warning"}}
	contextPods := make([]Pod, 0, len(problems))
	for index, problem := range problems {
		lines = append(lines, fmt.Sprintf("Priority %d: %s", index+1, problem.summary))
		facts = append(facts, Fact{Label: "Pod", Value: problem.pod.Name, Severity: problem.severity})
		facts = append(facts, Fact{Label: "Reason", Value: problem.reason, Severity: problem.severity})
		contextPods = append(contextPods, problem.pod)
	}

	return Response{
		Answer:      strings.Join(lines, ". ") + ".",
		Intent:      "cluster_priority_summary",
		Confidence:  ConfidenceHigh,
		Engine:      s.deterministicEngine(),
		Facts:       limitFacts(facts, 20),
		Queries:     []string{"kubernetes.pods"},
		Suggestions: podNameSuggestions(contextPods),
	}, nil
}

func (s *Service) answerPrioritySummaryFromMetrics(ctx context.Context) (Response, error) {
	if s.prometheus == nil {
		return Response{
			Answer:      "I could not list pods from Kubernetes MCP right now, so I could not build the full priority report. Try again in a moment or ask a focused metric question.",
			Intent:      "cluster_priority_summary",
			Confidence:  ConfidenceLow,
			Engine:      s.deterministicEngine(),
			Facts:       []Fact{{Label: "Priority report", Value: "Kubernetes MCP unavailable", Severity: "warning"}},
			Queries:     []string{"kubernetes.pods"},
			Suggestions: defaultSuggestions(),
		}, nil
	}

	orderedIntents := []string{"pod_crashloops", "pod_image_pull_errors", "pod_pending", "pod_restarts", "node_ready"}
	lines := []string{}
	facts := []Fact{{Label: "Priority report", Value: "Prometheus fallback", Severity: "warning"}}
	queries := []string{"kubernetes.pods"}
	for _, intentID := range orderedIntents {
		intent := intentByID(intentID)
		if intent == nil {
			continue
		}
		query := intent.query(s.namespace)
		queries = append(queries, query)
		raw, err := s.prometheus.InstantQuery(ctx, query)
		if err != nil {
			continue
		}
		result, err := readVector(raw)
		if err != nil {
			continue
		}
		count := int(math.Round(result.total))
		names := result.nonzeroLabels(intent.seriesLabel)
		severity, answer := formatIntentAnswer(intent, count, s.namespace, names)
		facts = append(facts, Fact{Label: intent.label, Value: fmt.Sprintf("%d", count), Severity: severity})
		if metricIntentNeedsAttention(intent, count) {
			lines = append(lines, answer)
		}
	}

	if len(lines) == 0 {
		lines = append(lines, "I could not list pods from Kubernetes MCP, but the Prometheus checks I could run did not show urgent pod problems.")
	} else {
		lines = append([]string{"Kubernetes pod listing timed out, so I used Prometheus metrics for this summary."}, lines...)
	}
	return Response{
		Answer:      strings.Join(lines, " "),
		Intent:      "cluster_priority_summary",
		Confidence:  ConfidenceLow,
		Engine:      s.deterministicEngine(),
		Facts:       limitFacts(facts, 12),
		Queries:     queries,
		Suggestions: []string{"Any crash loops?", "Any image pull errors?", "Which pods are restarting?"},
	}, nil
}

func metricIntentNeedsAttention(intent *intentDefinition, count int) bool {
	if intent.healthyWhenPositive {
		return count == 0
	}
	return count > 0
}

func (s *Service) answerNamespaces(ctx context.Context) (Response, error) {
	if s.kubernetes == nil {
		return Response{
			Answer:      "I can answer namespace questions, but Kubernetes namespace listing is not configured for this backend yet.",
			Intent:      IntentUnsupported,
			Confidence:  ConfidenceLow,
			Engine:      s.deterministicEngine(),
			Facts:       []Fact{},
			Queries:     []string{},
			Suggestions: defaultSuggestions(),
		}, nil
	}

	namespaces, err := s.kubernetes.Namespaces(ctx)
	if err != nil {
		return Response{
			Answer:      "I could not list namespaces from Kubernetes MCP right now. Try again in a moment.",
			Intent:      "namespaces",
			Confidence:  ConfidenceLow,
			Engine:      s.deterministicEngine(),
			Facts:       []Fact{{Label: "Namespaces", Value: "unavailable", Severity: "warning"}},
			Queries:     []string{"kubernetes.namespaces"},
			Suggestions: []string{"List all pods", "Are my pods healthy?", "Are nodes ready?"},
		}, nil
	}
	if len(namespaces) == 0 {
		return Response{
			Answer:      "I did not find any namespaces in the cluster.",
			Intent:      "namespaces",
			Confidence:  ConfidenceHigh,
			Engine:      s.deterministicEngine(),
			Facts:       []Fact{{Label: "Namespaces", Value: "0", Severity: "warning"}},
			Queries:     []string{"kubernetes.namespaces"},
			Suggestions: []string{"List all pods", "Are nodes ready?", "Any crash loops?"},
		}, nil
	}

	names := make([]string, 0, len(namespaces))
	facts := []Fact{{Label: "Namespaces", Value: fmt.Sprintf("%d", len(namespaces)), Severity: "healthy"}}
	for _, namespace := range namespaces {
		name := strings.TrimSpace(namespace.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
		facts = append(facts, Fact{Label: "Namespace", Value: name, Severity: namespaceSeverity(namespace)})
	}
	return Response{
		Answer:      fmt.Sprintf("I found %d namespace(s): %s.", len(names), strings.Join(names, ", ")),
		Intent:      "namespaces",
		Confidence:  ConfidenceHigh,
		Engine:      s.deterministicEngine(),
		Facts:       limitFacts(facts, 24),
		Queries:     []string{"kubernetes.namespaces"},
		Suggestions: []string{"List all pods", "Are my pods healthy?", "Are nodes ready?"},
	}, nil
}

func (s *Service) answerAllPods(ctx context.Context) (Response, error) {
	if s.kubernetes == nil {
		return Response{
			Answer:      "I can count pods, but Kubernetes pod listing is not configured for this backend yet.",
			Intent:      IntentUnsupported,
			Confidence:  ConfidenceLow,
			Engine:      s.deterministicEngine(),
			Facts:       []Fact{},
			Queries:     []string{},
			Suggestions: defaultSuggestions(),
		}, nil
	}

	pods, err := s.kubernetes.Pods(ctx, s.namespace)
	if err != nil {
		return Response{}, fmt.Errorf("list pods from kubernetes mcp: %w", err)
	}
	if len(pods) == 0 {
		return Response{
			Answer:      fmt.Sprintf("I did not find any pods in %s.", s.namespace),
			Intent:      "all_pods",
			Confidence:  ConfidenceHigh,
			Engine:      s.deterministicEngine(),
			Facts:       []Fact{{Label: "Pods", Value: "0", Severity: "warning"}},
			Queries:     []string{"kubernetes.pods"},
			Suggestions: []string{"Are my pods healthy?", "Any pending pods?", "Are nodes ready?"},
		}, nil
	}

	names := make([]string, 0, len(pods))
	facts := []Fact{{Label: "Pods", Value: fmt.Sprintf("%d", len(pods)), Severity: "healthy"}}
	for _, pod := range pods {
		names = append(names, describePodShort(pod))
		facts = append(facts, Fact{Label: "Pod", Value: pod.Name, Severity: severityForPod(pod)})
	}
	return Response{
		Answer:      fmt.Sprintf("I found %d pod(s) in %s: %s.", len(pods), s.namespace, strings.Join(names, "; ")),
		Intent:      "all_pods",
		Confidence:  ConfidenceHigh,
		Engine:      s.deterministicEngine(),
		Facts:       limitFacts(facts, 24),
		Queries:     []string{"kubernetes.pods"},
		Suggestions: []string{"Which pods are failing?", "Any crash loops?", "Show details for a pod name"},
	}, nil
}

func (s *Service) answerUnhealthyPods(ctx context.Context) (Response, error) {
	if s.kubernetes == nil {
		return Response{
			Answer:      "I can count unhealthy pods, but Kubernetes pod listing is not configured for this backend yet.",
			Intent:      IntentUnsupported,
			Confidence:  ConfidenceLow,
			Engine:      s.deterministicEngine(),
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
			Engine:      s.deterministicEngine(),
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
		Engine:      s.deterministicEngine(),
		Facts:       limitFacts(facts, 18),
		Queries:     []string{"kubernetes.pods"},
		Suggestions: podNameSuggestions(unhealthy),
	}, nil
}

func (s *Service) answerPodDetail(ctx context.Context, originalMessage, normalizedMessage string, chatContext Context) (Response, error) {
	if s.kubernetes == nil {
		return Response{
			Answer:      "I can identify cluster health issues, but Kubernetes pod details are not configured for this backend yet.",
			Intent:      IntentUnsupported,
			Confidence:  ConfidenceLow,
			Engine:      s.deterministicEngine(),
			Facts:       []Fact{},
			Queries:     []string{},
			Suggestions: []string{"Any crash loops?", "Any pending pods?", "Any image pull errors?"},
		}, nil
	}

	pods, err := s.kubernetes.Pods(ctx, s.namespace)
	if err != nil {
		return Response{}, fmt.Errorf("list pods from kubernetes mcp: %w", err)
	}
	pod, ok := podFromContext(normalizedMessage, pods, chatContext)
	if !ok {
		pod, ok = matchPodFromMessage(originalMessage, normalizedMessage, pods)
	}
	if !ok {
		answer := "I could not match that question to a pod name in the monitoring-tool namespace."
		if len(pods) > 0 {
			answer = fmt.Sprintf("%s I can see pods such as: %s.", answer, strings.Join(firstPodNames(pods, 6), ", "))
		}
		return Response{
			Answer:      answer,
			Intent:      "pod_details",
			Confidence:  ConfidenceLow,
			Engine:      s.deterministicEngine(),
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
		Engine:      s.deterministicEngine(),
		Facts:       podFacts(pod),
		Queries:     []string{"kubernetes.pods", "kubernetes.events", "kubernetes.logs"},
		Suggestions: []string{"Any crash loops?", "Any image pull errors?", "Which pods are restarting?"},
	}, nil
}

func podFromContext(message string, pods []Pod, chatContext Context) (Pod, bool) {
	podNames := cleanContextPods(chatContext.Pods)
	if len(podNames) == 0 {
		return Pod{}, false
	}

	index := -1
	switch {
	case strings.Contains(message, "first one"), strings.Contains(message, "first pod"):
		index = 0
	case strings.Contains(message, "second one"), strings.Contains(message, "second pod"):
		index = 1
	case strings.Contains(message, "third one"), strings.Contains(message, "third pod"):
		index = 2
	case strings.Contains(message, " it"), message == "it", strings.Contains(message, "that pod"), strings.Contains(message, "this pod"):
		index = 0
	}
	if index < 0 || index >= len(podNames) {
		return Pod{}, false
	}
	return findPodByName(pods, podNames[index])
}

func cleanContextPods(values []string) []string {
	seen := map[string]bool{}
	pods := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		pods = append(pods, value)
		if len(pods) == 8 {
			break
		}
	}
	return pods
}

func findPodByName(pods []Pod, name string) (Pod, bool) {
	for _, pod := range pods {
		if pod.Name == name {
			return pod, true
		}
	}
	return Pod{}, false
}

type podProblem struct {
	pod      Pod
	score    int
	reason   string
	severity string
	summary  string
}

func rankedPodProblems(pods []Pod) []podProblem {
	problems := []podProblem{}
	for _, pod := range pods {
		problem, ok := podProblemFor(pod)
		if ok {
			problems = append(problems, problem)
		}
	}
	for i := 0; i < len(problems); i++ {
		for j := i + 1; j < len(problems); j++ {
			if problems[j].score > problems[i].score {
				problems[i], problems[j] = problems[j], problems[i]
			}
		}
	}
	return problems
}

func podProblemFor(pod Pod) (podProblem, bool) {
	if pod.Name == "" {
		return podProblem{}, false
	}
	waiting := strings.Join(pod.WaitingReasons, ", ")
	if containsWaitingReason(pod, "CrashLoopBackOff") {
		return newPodProblem(pod, 100, "CrashLoopBackOff", "error", fmt.Sprintf("%s is in CrashLoopBackOff with %d restart(s)", pod.Name, pod.RestartCount)), true
	}
	if containsWaitingReason(pod, "ImagePullBackOff") || containsWaitingReason(pod, "ErrImagePull") {
		return newPodProblem(pod, 90, emptyFallback(waiting, "ImagePullBackOff"), "error", fmt.Sprintf("%s has image pull failure: %s", pod.Name, emptyFallback(waiting, "image pull error"))), true
	}
	if pod.Phase == "Pending" {
		return newPodProblem(pod, 80, "Pending", "warning", fmt.Sprintf("%s is Pending and may be unschedulable", pod.Name)), true
	}
	if pod.Phase == "Failed" || pod.Phase == "Unknown" {
		return newPodProblem(pod, 75, pod.Phase, "error", fmt.Sprintf("%s is %s", pod.Name, pod.Phase)), true
	}
	if len(pod.WaitingReasons) > 0 {
		return newPodProblem(pod, 70, waiting, "warning", fmt.Sprintf("%s is waiting: %s", pod.Name, waiting)), true
	}
	if pod.RestartCount > 0 {
		score := 50 + pod.RestartCount
		return newPodProblem(pod, score, "Restarts", "warning", fmt.Sprintf("%s restarted %d time(s)", pod.Name, pod.RestartCount)), true
	}
	return podProblem{}, false
}

func newPodProblem(pod Pod, score int, reason, severity, summary string) podProblem {
	return podProblem{pod: pod, score: score, reason: reason, severity: severity, summary: summary}
}

func containsWaitingReason(pod Pod, reason string) bool {
	for _, waitingReason := range pod.WaitingReasons {
		if strings.EqualFold(waitingReason, reason) {
			return true
		}
	}
	return false
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

func namespaceSeverity(namespace Namespace) string {
	if namespace.Phase != "" && namespace.Phase != "Active" {
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
		keywords:    []string{"crash loop", "crash back loop", "crashbackloop", "crashloop", "crashloopbackoff"},
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
