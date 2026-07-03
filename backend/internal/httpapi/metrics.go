package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	promclient "monitoring-tool/backend/internal/prometheus"
)

const (
	maxQueryLength     = 4096
	maxRange           = 24 * time.Hour
	maxSamplesPerQuery = 1200
	defaultTimeout     = 10 * time.Second
	maxTimeout         = 30 * time.Second
)

type MetricsHandler struct {
	prometheus *promclient.Client
}

func NewMetricsHandler(prometheus *promclient.Client) *MetricsHandler {
	return &MetricsHandler{prometheus: prometheus}
}

func (h *MetricsHandler) Query(w http.ResponseWriter, r *http.Request) {
	query, timeout, err := parseInstantRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	data, err := h.prometheus.InstantQuery(ctx, query)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *MetricsHandler) QueryRange(w http.ResponseWriter, r *http.Request) {
	req, err := parseRangeRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), req.Timeout)
	defer cancel()

	data, err := h.prometheus.RangeQuery(ctx, req.Query, req.Start, req.End, req.Step)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *MetricsHandler) Labels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), timeoutFromRequest(r))
	defer cancel()

	data, err := h.prometheus.Labels(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *MetricsHandler) LabelValues(w http.ResponseWriter, r *http.Request) {
	label := strings.TrimSpace(r.URL.Query().Get("label"))
	if label == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("label is required"))
		return
	}
	if strings.ContainsAny(label, `/\{}[](),"`) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("label contains invalid characters"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeoutFromRequest(r))
	defer cancel()

	data, err := h.prometheus.LabelValues(ctx, label)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *MetricsHandler) Series(w http.ResponseWriter, r *http.Request) {
	req, err := parseSeriesRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), req.Timeout)
	defer cancel()

	data, err := h.prometheus.Series(ctx, req.Matches, req.Start, req.End)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

type rangeRequest struct {
	Query   string
	Start   time.Time
	End     time.Time
	Step    time.Duration
	Timeout time.Duration
}

type seriesRequest struct {
	Matches []string
	Start   time.Time
	End     time.Time
	Timeout time.Duration
}

func parseInstantRequest(r *http.Request) (string, time.Duration, error) {
	query, err := queryFromRequest(r)
	if err != nil {
		return "", 0, err
	}
	return query, timeoutFromRequest(r), nil
}

func parseRangeRequest(r *http.Request) (rangeRequest, error) {
	query, err := queryFromRequest(r)
	if err != nil {
		return rangeRequest{}, err
	}

	start, err := parseUnixQueryTime(r.URL.Query().Get("start"), "start")
	if err != nil {
		return rangeRequest{}, err
	}
	end, err := parseUnixQueryTime(r.URL.Query().Get("end"), "end")
	if err != nil {
		return rangeRequest{}, err
	}
	step, err := parseStep(r.URL.Query().Get("step"))
	if err != nil {
		return rangeRequest{}, err
	}
	if !end.After(start) {
		return rangeRequest{}, fmt.Errorf("end must be after start")
	}
	if end.Sub(start) > maxRange {
		return rangeRequest{}, fmt.Errorf("range cannot exceed %s", maxRange)
	}
	if int(end.Sub(start)/step) > maxSamplesPerQuery {
		return rangeRequest{}, fmt.Errorf("range and step exceed sample limit of %d", maxSamplesPerQuery)
	}

	return rangeRequest{
		Query:   query,
		Start:   start,
		End:     end,
		Step:    step,
		Timeout: timeoutFromRequest(r),
	}, nil
}

func parseSeriesRequest(r *http.Request) (seriesRequest, error) {
	matches := r.URL.Query()["match[]"]
	if len(matches) == 0 {
		return seriesRequest{}, fmt.Errorf("at least one match[] is required")
	}
	if len(matches) > 10 {
		return seriesRequest{}, fmt.Errorf("at most 10 match[] values are allowed")
	}
	for _, match := range matches {
		if strings.TrimSpace(match) == "" || len(match) > maxQueryLength {
			return seriesRequest{}, fmt.Errorf("match[] contains an invalid selector")
		}
	}

	end := time.Now().UTC()
	start := end.Add(-time.Hour)
	var err error
	if raw := r.URL.Query().Get("start"); raw != "" {
		start, err = parseUnixQueryTime(raw, "start")
		if err != nil {
			return seriesRequest{}, err
		}
	}
	if raw := r.URL.Query().Get("end"); raw != "" {
		end, err = parseUnixQueryTime(raw, "end")
		if err != nil {
			return seriesRequest{}, err
		}
	}
	if !end.After(start) {
		return seriesRequest{}, fmt.Errorf("end must be after start")
	}
	if end.Sub(start) > maxRange {
		return seriesRequest{}, fmt.Errorf("range cannot exceed %s", maxRange)
	}

	return seriesRequest{Matches: matches, Start: start, End: end, Timeout: timeoutFromRequest(r)}, nil
}

func queryFromRequest(r *http.Request) (string, error) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	if len(query) > maxQueryLength {
		return "", fmt.Errorf("query cannot exceed %d characters", maxQueryLength)
	}
	return query, nil
}

func parseUnixQueryTime(raw, name string) (time.Time, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be a Unix timestamp", name)
	}
	return time.UnixMilli(int64(value * 1000)).UTC(), nil
}

func parseStep(raw string) (time.Duration, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("step must be a positive number of seconds")
	}
	step := time.Duration(value * float64(time.Second))
	if step < time.Second {
		return 0, fmt.Errorf("step must be at least 1 second")
	}
	return step, nil
}

func timeoutFromRequest(r *http.Request) time.Duration {
	raw := strings.TrimSpace(r.URL.Query().Get("timeout"))
	if raw == "" {
		return defaultTimeout
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultTimeout
	}
	timeout := time.Duration(seconds) * time.Second
	if timeout > maxTimeout {
		return maxTimeout
	}
	return timeout
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{
		"error": err.Error(),
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
