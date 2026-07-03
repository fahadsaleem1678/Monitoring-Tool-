package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

func (c *Client) Timeout() time.Duration {
	return c.timeout
}

func (c *Client) Ready(ctx context.Context) error {
	if err := c.get(ctx, "/-/ready", nil, nil); err == nil {
		return nil
	}

	var queryResponse queryEnvelope
	if err := c.get(ctx, "/api/v1/query", url.Values{"query": []string{"up"}}, &queryResponse); err != nil {
		return err
	}
	if queryResponse.Status != "success" {
		return fmt.Errorf("prometheus returned status %q", queryResponse.Status)
	}
	return nil
}

func (c *Client) InstantQuery(ctx context.Context, query string) (json.RawMessage, error) {
	var response queryEnvelope
	if err := c.get(ctx, "/api/v1/query", url.Values{"query": []string{query}}, &response); err != nil {
		return nil, err
	}
	if response.Status != "success" {
		return nil, fmt.Errorf("prometheus returned status %q", response.Status)
	}
	return response.Data, nil
}

func (c *Client) RangeQuery(ctx context.Context, query string, start, end time.Time, step time.Duration) (json.RawMessage, error) {
	params := url.Values{
		"query": []string{query},
		"start": []string{formatUnixTime(start)},
		"end":   []string{formatUnixTime(end)},
		"step":  []string{formatDurationSeconds(step)},
	}

	var response queryEnvelope
	if err := c.get(ctx, "/api/v1/query_range", params, &response); err != nil {
		return nil, err
	}
	if response.Status != "success" {
		return nil, fmt.Errorf("prometheus returned status %q", response.Status)
	}
	return response.Data, nil
}

func (c *Client) Labels(ctx context.Context) (json.RawMessage, error) {
	var response queryEnvelope
	if err := c.get(ctx, "/api/v1/labels", nil, &response); err != nil {
		return nil, err
	}
	if response.Status != "success" {
		return nil, fmt.Errorf("prometheus returned status %q", response.Status)
	}
	return response.Data, nil
}

func (c *Client) LabelValues(ctx context.Context, label string) (json.RawMessage, error) {
	var response queryEnvelope
	if err := c.get(ctx, "/api/v1/label/"+url.PathEscape(label)+"/values", nil, &response); err != nil {
		return nil, err
	}
	if response.Status != "success" {
		return nil, fmt.Errorf("prometheus returned status %q", response.Status)
	}
	return response.Data, nil
}

func (c *Client) Series(ctx context.Context, matches []string, start, end time.Time) (json.RawMessage, error) {
	params := url.Values{
		"start": []string{formatUnixTime(start)},
		"end":   []string{formatUnixTime(end)},
	}
	for _, match := range matches {
		params.Add("match[]", match)
	}

	var response queryEnvelope
	if err := c.get(ctx, "/api/v1/series", params, &response); err != nil {
		return nil, err
	}
	if response.Status != "success" {
		return nil, fmt.Errorf("prometheus returned status %q", response.Status)
	}
	return response.Data, nil
}

func (c *Client) get(ctx context.Context, path string, params url.Values, target any) error {
	if c.baseURL == "" {
		return fmt.Errorf("PROMETHEUS_URL is not configured")
	}

	endpoint, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("invalid prometheus url: %w", err)
	}
	if params != nil {
		endpoint.RawQuery = params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("prometheus returned HTTP %d", res.StatusCode)
	}

	if target == nil {
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(target); err != nil {
		return fmt.Errorf("decode prometheus response: %w", err)
	}
	return nil
}

type queryEnvelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

func formatUnixTime(value time.Time) string {
	return strconv.FormatFloat(float64(value.UnixMilli())/1000, 'f', 3, 64)
}

func formatDurationSeconds(value time.Duration) string {
	return strconv.FormatFloat(value.Seconds(), 'f', 0, 64)
}
