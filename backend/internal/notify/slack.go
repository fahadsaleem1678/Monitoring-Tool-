package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: strings.TrimSpace(webhookURL),
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *SlackNotifier) Configured() bool {
	return n.webhookURL != ""
}

func (n *SlackNotifier) Send(ctx context.Context, message string) error {
	if !n.Configured() {
		return fmt.Errorf("SLACK_WEBHOOK_URL is not configured")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("slack message is required")
	}

	body, err := json.Marshal(map[string]string{"text": message})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	responseBody, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if text := strings.TrimSpace(string(responseBody)); text != "" && text != "ok" {
		return fmt.Errorf("slack webhook returned unexpected response: %s", text)
	}
	return nil
}
