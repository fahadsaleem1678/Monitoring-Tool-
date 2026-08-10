package kubernetesmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Pod struct {
	Namespace      string   `json:"namespace"`
	Name           string   `json:"name"`
	Phase          string   `json:"phase"`
	RestartCount   int      `json:"restart_count"`
	WaitingReasons []string `json:"waiting_reasons"`
}

type Event struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Type          string `json:"type"`
	LastTimestamp string `json:"last_timestamp"`
}

type Logs struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Preview   string `json:"preview"`
}

func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Pods(ctx context.Context, namespace string) ([]Pod, error) {
	var response struct {
		Data struct {
			Pods []Pod `json:"pods"`
		} `json:"data"`
	}
	if err := c.postTool(ctx, "kubernetes.pods", map[string]any{"namespace": namespace}, &response); err != nil {
		return nil, err
	}
	return response.Data.Pods, nil
}

func (c *Client) Events(ctx context.Context, namespace string) ([]Event, error) {
	var response struct {
		Data struct {
			Events []Event `json:"events"`
		} `json:"data"`
	}
	if err := c.postTool(ctx, "kubernetes.events", map[string]any{"namespace": namespace}, &response); err != nil {
		return nil, err
	}
	return response.Data.Events, nil
}

func (c *Client) Logs(ctx context.Context, namespace, pod string, tailLines int) (Logs, error) {
	var response struct {
		Data Logs `json:"data"`
	}
	payload := map[string]any{"namespace": namespace, "pod": pod, "tail_lines": tailLines}
	if err := c.postTool(ctx, "kubernetes.logs", payload, &response); err != nil {
		return Logs{}, err
	}
	return response.Data, nil
}

func (c *Client) postTool(ctx context.Context, tool string, payload map[string]any, target any) error {
	if c.baseURL == "" {
		return fmt.Errorf("KUBERNETES_MCP_URL is not configured")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tools/"+tool, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("kubernetes mcp returned HTTP %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(target); err != nil {
		return fmt.Errorf("decode kubernetes mcp response: %w", err)
	}
	return nil
}
