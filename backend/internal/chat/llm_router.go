package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type LLMRouterConfig struct {
	Enabled  bool
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
	Timeout  time.Duration
}

type LLMIntentRouter struct {
	config LLMRouterConfig
	client *http.Client
}

func NewLLMIntentRouter(config LLMRouterConfig) *LLMIntentRouter {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &LLMIntentRouter{
		config: config,
		client: &http.Client{Timeout: timeout},
	}
}

func (r *LLMIntentRouter) Route(ctx context.Context, message string, chatContext Context, intents []string) (string, error) {
	if !r.config.Enabled {
		return "", nil
	}
	if !r.ready() {
		return "", nil
	}

	messages := buildRouterMessages(message, chatContext, intents)
	provider := strings.ToLower(strings.TrimSpace(r.config.Provider))
	var content string
	var err error
	switch provider {
	case "openai":
		content, err = r.openAIChat(ctx, messages)
	case "ollama":
		content, err = r.ollamaChat(ctx, messages)
	default:
		return "", fmt.Errorf("unsupported assistant llm provider %q", provider)
	}
	if err != nil {
		slog.Warn("assistant llm route failed", "provider", provider, "model", r.config.Model, "error", err)
		return "", err
	}

	intent, err := parseRoutedIntent(content)
	if err != nil {
		slog.Warn("assistant llm route parse failed", "provider", provider, "model", r.config.Model, "error", err, "content", singleLinePreview(content, 180))
		return "", err
	}
	if !allowedIntent(intent, intents) {
		return IntentUnsupported, nil
	}
	return intent, nil
}

func (r *LLMIntentRouter) AnswerGeneral(ctx context.Context, message string, chatContext Context) (string, error) {
	if !r.config.Enabled || !r.ready() {
		return "", fmt.Errorf("assistant llm is not configured")
	}

	messages := buildGeneralAnswerMessages(message, chatContext)
	provider := strings.ToLower(strings.TrimSpace(r.config.Provider))
	var content string
	var err error
	switch provider {
	case "openai":
		content, err = r.openAIChatWithFormat(ctx, messages, false)
	case "ollama":
		content, err = r.ollamaChatPlain(ctx, messages)
	default:
		return "", fmt.Errorf("unsupported assistant llm provider %q", provider)
	}
	if err != nil {
		slog.Warn("assistant llm general answer failed", "provider", provider, "model", r.config.Model, "error", err)
		return "", err
	}

	answer := strings.TrimSpace(content)
	if answer == "" {
		return "", fmt.Errorf("assistant llm returned empty general answer")
	}
	return singleLinePreview(answer, 900), nil
}

func (r *LLMIntentRouter) ready() bool {
	return strings.TrimSpace(r.config.Provider) != "" && strings.TrimSpace(r.config.Model) != ""
}

func (r *LLMIntentRouter) openAIChat(ctx context.Context, messages []routerMessage) (string, error) {
	content, err := r.openAIChatWithFormat(ctx, messages, true)
	if err == nil {
		return content, nil
	}
	slog.Warn("assistant llm route retrying without response_format", "model", r.config.Model, "error", err)
	return r.openAIChatWithFormat(ctx, messages, false)
}

func (r *LLMIntentRouter) openAIChatWithFormat(ctx context.Context, messages []routerMessage, responseFormat bool) (string, error) {
	if strings.TrimSpace(r.config.APIKey) == "" {
		return "", fmt.Errorf("OPENAI_API_KEY is required when assistant LLM provider is openai")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(r.config.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	payload := map[string]any{
		"model":       r.config.Model,
		"messages":    messages,
		"temperature": 0,
	}
	if responseFormat {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+r.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
		return "", fmt.Errorf("assistant llm returned HTTP %d: %s", resp.StatusCode, singleLinePreview(string(body), 300))
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("assistant llm returned no choices")
	}
	return decoded.Choices[0].Message.Content, nil
}

func (r *LLMIntentRouter) ollamaChat(ctx context.Context, messages []routerMessage) (string, error) {
	return r.ollamaChatWithFormat(ctx, messages, "json")
}

func (r *LLMIntentRouter) ollamaChatPlain(ctx context.Context, messages []routerMessage) (string, error) {
	return r.ollamaChatWithFormat(ctx, messages, "")
}

func (r *LLMIntentRouter) ollamaChatWithFormat(ctx context.Context, messages []routerMessage, format string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(r.config.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://host.docker.internal:11434"
	}
	payload := map[string]any{
		"model":    r.config.Model,
		"messages": messages,
		"stream":   false,
		"options":  map[string]any{"temperature": 0, "num_predict": 80},
	}
	if format != "" {
		payload["format"] = format
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("assistant llm returned HTTP %d", resp.StatusCode)
	}
	var decoded struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}
	return decoded.Message.Content, nil
}

type routerMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func buildRouterMessages(message string, chatContext Context, intents []string) []routerMessage {
	system := strings.Join([]string{
		"You are a Kubernetes monitoring assistant router.",
		"Return JSON only.",
		"Choose exactly one approved intent for the user's question.",
		"Do not answer the user, write commands, or invent a new intent.",
		"If the question asks to change infrastructure or is outside read-only cluster health, choose unsupported.",
	}, " ")
	user := map[string]any{
		"question":         message,
		"previous_pods":    chatContext.Pods,
		"previous_intent":  chatContext.LastIntent,
		"approved_intents": intents,
		"json_shape":       map[string]string{"intent": "one approved intent id"},
	}
	userJSON, _ := json.Marshal(user)
	return []routerMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: string(userJSON)},
	}
}

func buildGeneralAnswerMessages(message string, chatContext Context) []routerMessage {
	system := strings.Join([]string{
		"You are a concise Kubernetes and monitoring assistant.",
		"Answer general conceptual questions only.",
		"Do not claim to have checked the live cluster unless tool facts are provided.",
		"Do not provide instructions that mutate infrastructure, secrets, credentials, or production state.",
		"If the user asks for live cluster status, say they should ask a supported cluster check.",
		"Keep the answer under 120 words.",
	}, " ")
	user := map[string]any{
		"question":        message,
		"previous_pods":   chatContext.Pods,
		"previous_intent": chatContext.LastIntent,
	}
	userJSON, _ := json.Marshal(user)
	return []routerMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: string(userJSON)},
	}
}

func parseRoutedIntent(content string) (string, error) {
	var decoded struct {
		Intent string `json:"intent"`
	}
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &decoded); err != nil {
		return "", err
	}
	return strings.TrimSpace(decoded.Intent), nil
}

func extractJSONObject(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		return trimmed
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func allowedIntent(intent string, intents []string) bool {
	for _, candidate := range intents {
		if intent == candidate {
			return true
		}
	}
	return false
}
