package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"monitoring-tool/backend/internal/notify"
	"monitoring-tool/backend/internal/store"
)

type AlertHandler struct {
	store *store.Store
	slack *notify.SlackNotifier
}

func NewAlertHandler(store *store.Store, slack *notify.SlackNotifier) *AlertHandler {
	return &AlertHandler{store: store, slack: slack}
}

type alertRuleRequest struct {
	Name       string  `json:"name"`
	PromQL     string  `json:"promql"`
	Operator   string  `json:"operator"`
	Threshold  float64 `json:"threshold"`
	ForSeconds int     `json:"for_seconds"`
	Severity   string  `json:"severity"`
	Enabled    *bool   `json:"enabled"`
}

type testNotificationRequest struct {
	Message string `json:"message"`
}

func (h *AlertHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListAlertRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (h *AlertHandler) CreateRule(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeAlertRuleRequest(w, r)
	if !ok {
		return
	}
	rule, err := h.store.CreateAlertRule(r.Context(), alertRuleFromRequest(req, uuid.Nil))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"rule": rule})
}

func (h *AlertHandler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	id, ok := alertRuleIDFromPath(w, r)
	if !ok {
		return
	}
	req, ok := decodeAlertRuleRequest(w, r)
	if !ok {
		return
	}
	rule, err := h.store.UpdateAlertRule(r.Context(), alertRuleFromRequest(req, id))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": rule})
}

func (h *AlertHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := alertRuleIDFromPath(w, r)
	if !ok {
		return
	}
	if err := h.store.DeleteAlertRule(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AlertHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("limit must be a number"))
			return
		}
		limit = parsed
	}
	events, err := h.store.ListAlertEvents(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *AlertHandler) TestNotification(w http.ResponseWriter, r *http.Request) {
	var req testNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "Monitoring Tool test alert: Slack notifications are connected."
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := h.slack.Send(ctx, message); err != nil {
		_, _ = h.store.CreateAlertEvent(r.Context(), store.AlertEvent{
			Status:  "notification_failed",
			Message: fmt.Sprintf("Slack test notification failed: %s", err.Error()),
		})
		writeError(w, http.StatusBadGateway, err)
		return
	}

	event, err := h.store.CreateAlertEvent(r.Context(), store.AlertEvent{
		Status:  "notification_sent",
		Message: message,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "event": event})
}

func decodeAlertRuleRequest(w http.ResponseWriter, r *http.Request) (alertRuleRequest, bool) {
	var req alertRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return alertRuleRequest{}, false
	}
	req.Name = strings.TrimSpace(req.Name)
	req.PromQL = strings.TrimSpace(req.PromQL)
	req.Operator = strings.TrimSpace(req.Operator)
	req.Severity = strings.ToLower(strings.TrimSpace(req.Severity))
	if req.Name == "" || req.PromQL == "" {
		writeError(w, http.StatusBadRequest, errors.New("name and promql are required"))
		return alertRuleRequest{}, false
	}
	if !validAlertOperator(req.Operator) {
		writeError(w, http.StatusBadRequest, errors.New("operator must be one of >, >=, <, <=, ==, !="))
		return alertRuleRequest{}, false
	}
	if req.ForSeconds <= 0 {
		req.ForSeconds = 60
	}
	if req.Severity == "" {
		req.Severity = "warning"
	}
	if req.Severity != "info" && req.Severity != "warning" && req.Severity != "critical" {
		writeError(w, http.StatusBadRequest, errors.New("severity must be info, warning, or critical"))
		return alertRuleRequest{}, false
	}
	return req, true
}

func alertRuleFromRequest(req alertRuleRequest, id uuid.UUID) store.AlertRule {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return store.AlertRule{
		ID:         id,
		Name:       req.Name,
		PromQL:     req.PromQL,
		Operator:   req.Operator,
		Threshold:  req.Threshold,
		ForSeconds: req.ForSeconds,
		Severity:   req.Severity,
		Enabled:    enabled,
	}
}

func validAlertOperator(operator string) bool {
	switch operator {
	case ">", ">=", "<", "<=", "==", "!=":
		return true
	default:
		return false
	}
}

func alertRuleIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid alert rule id"))
		return uuid.Nil, false
	}
	return id, true
}
