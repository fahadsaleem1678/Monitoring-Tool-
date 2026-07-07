package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"monitoring-tool/backend/internal/notify"
	promclient "monitoring-tool/backend/internal/prometheus"
	"monitoring-tool/backend/internal/store"
)

const minEvaluationInterval = 5 * time.Second

type Evaluator struct {
	store      *store.Store
	prometheus *promclient.Client
	notifier   *notify.SlackNotifier
	logger     *slog.Logger
	interval   time.Duration

	mu           sync.Mutex
	pendingSince map[uuid.UUID]time.Time
}

func NewEvaluator(store *store.Store, prometheus *promclient.Client, notifier *notify.SlackNotifier, logger *slog.Logger, interval time.Duration) *Evaluator {
	if interval < minEvaluationInterval {
		interval = minEvaluationInterval
	}
	return &Evaluator{
		store:        store,
		prometheus:   prometheus,
		notifier:     notifier,
		logger:       logger,
		interval:     interval,
		pendingSince: map[uuid.UUID]time.Time{},
	}
}

func (e *Evaluator) Run(ctx context.Context) {
	e.logger.Info("alert evaluator starting", "interval", e.interval.String())
	e.Evaluate(ctx)

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("alert evaluator stopped")
			return
		case <-ticker.C:
			e.Evaluate(ctx)
		}
	}
}

func (e *Evaluator) Evaluate(ctx context.Context) {
	rules, err := e.store.ListAlertRules(ctx)
	if err != nil {
		e.logger.Error("alert rule load failed", "error", err)
		return
	}

	now := time.Now()
	for _, rule := range rules {
		if !rule.Enabled {
			e.clearPending(rule.ID)
			e.resolveIfOpen(ctx, rule, nil, fmt.Sprintf("Alert %q resolved because the rule was disabled.", rule.Name))
			continue
		}
		e.evaluateRule(ctx, now, rule)
	}
}

func (e *Evaluator) evaluateRule(ctx context.Context, now time.Time, rule store.AlertRule) {
	queryCtx, cancel := context.WithTimeout(ctx, e.prometheus.Timeout())
	defer cancel()

	data, err := e.prometheus.InstantQuery(queryCtx, rule.PromQL)
	if err != nil {
		e.logger.Error("alert query failed", "rule_id", rule.ID.String(), "rule", rule.Name, "error", err)
		return
	}

	result, err := evaluatePrometheusResult(data, rule.Operator, rule.Threshold)
	if err != nil {
		e.logger.Error("alert result evaluation failed", "rule_id", rule.ID.String(), "rule", rule.Name, "error", err)
		return
	}

	openEvent, hasOpenEvent, err := e.store.OpenAlertEventByRuleID(ctx, rule.ID)
	if err != nil {
		e.logger.Error("open alert event lookup failed", "rule_id", rule.ID.String(), "rule", rule.Name, "error", err)
		return
	}

	if result.Firing {
		if hasOpenEvent {
			e.clearPending(rule.ID)
			return
		}
		if !e.pendingSatisfied(rule, now) {
			return
		}
		e.createFiringEvent(ctx, rule, result.Value)
		return
	}

	e.clearPending(rule.ID)
	if hasOpenEvent {
		e.resolveOpenEvent(ctx, rule, openEvent, result.Value)
	}
}

func (e *Evaluator) pendingSatisfied(rule store.AlertRule, now time.Time) bool {
	duration := time.Duration(rule.ForSeconds) * time.Second
	if duration <= 0 {
		return true
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	since, ok := e.pendingSince[rule.ID]
	if !ok {
		e.pendingSince[rule.ID] = now
		return false
	}
	return now.Sub(since) >= duration
}

func (e *Evaluator) clearPending(ruleID uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pendingSince, ruleID)
}

func (e *Evaluator) createFiringEvent(ctx context.Context, rule store.AlertRule, value *float64) {
	message := firingMessage(rule, value)
	_, err := e.store.CreateAlertEvent(ctx, store.AlertEvent{
		RuleID:  &rule.ID,
		Status:  "firing",
		Value:   value,
		Message: message,
	})
	if err != nil {
		e.logger.Error("alert event create failed", "rule_id", rule.ID.String(), "rule", rule.Name, "error", err)
		return
	}
	e.clearPending(rule.ID)
	e.sendSlack(ctx, message, rule)
}

func (e *Evaluator) resolveIfOpen(ctx context.Context, rule store.AlertRule, value *float64, message string) {
	openEvent, ok, err := e.store.OpenAlertEventByRuleID(ctx, rule.ID)
	if err != nil {
		e.logger.Error("open alert event lookup failed", "rule_id", rule.ID.String(), "rule", rule.Name, "error", err)
		return
	}
	if ok {
		e.resolveEvent(ctx, rule, openEvent, value, message)
	}
}

func (e *Evaluator) resolveOpenEvent(ctx context.Context, rule store.AlertRule, openEvent store.AlertEvent, value *float64) {
	e.resolveEvent(ctx, rule, openEvent, value, resolvedMessage(rule, value))
}

func (e *Evaluator) resolveEvent(ctx context.Context, rule store.AlertRule, openEvent store.AlertEvent, value *float64, message string) {
	if _, err := e.store.ResolveAlertEvent(ctx, openEvent.ID, value, message); err != nil {
		e.logger.Error("alert event resolve failed", "rule_id", rule.ID.String(), "rule", rule.Name, "event_id", openEvent.ID.String(), "error", err)
		return
	}
	e.sendSlack(ctx, message, rule)
}

func (e *Evaluator) sendSlack(ctx context.Context, message string, rule store.AlertRule) {
	if e.notifier == nil || !e.notifier.Configured() {
		e.logger.Warn("slack notification skipped; webhook is not configured", "rule_id", rule.ID.String(), "rule", rule.Name)
		return
	}

	notifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := e.notifier.Send(notifyCtx, message); err != nil {
		e.logger.Error("slack notification failed", "rule_id", rule.ID.String(), "rule", rule.Name, "error", err)
	}
}

type evaluationResult struct {
	Firing bool
	Value  *float64
}

func evaluatePrometheusResult(data json.RawMessage, operator string, threshold float64) (evaluationResult, error) {
	values, err := sampleValues(data)
	if err != nil {
		return evaluationResult{}, err
	}

	var selected *float64
	for _, value := range values {
		if compare(value, operator, threshold) {
			selected = betterAlertValue(selected, value, operator)
			continue
		}
		if selected == nil {
			v := value
			selected = &v
		}
	}
	return evaluationResult{Firing: anyMatches(values, operator, threshold), Value: selected}, nil
}

func anyMatches(values []float64, operator string, threshold float64) bool {
	for _, value := range values {
		if compare(value, operator, threshold) {
			return true
		}
	}
	return false
}

func compare(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

func betterAlertValue(current *float64, candidate float64, operator string) *float64 {
	if current == nil {
		value := candidate
		return &value
	}
	switch operator {
	case "<", "<=":
		if candidate < *current {
			value := candidate
			return &value
		}
	default:
		if candidate > *current {
			value := candidate
			return &value
		}
	}
	return current
}

func sampleValues(data json.RawMessage) ([]float64, error) {
	var envelope struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode query data: %w", err)
	}

	switch envelope.ResultType {
	case "vector":
		var samples []struct {
			Value []json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(envelope.Result, &samples); err != nil {
			return nil, fmt.Errorf("decode vector result: %w", err)
		}
		values := make([]float64, 0, len(samples))
		for _, sample := range samples {
			value, ok, err := parsePrometheusValue(sample.Value)
			if err != nil {
				return nil, err
			}
			if ok {
				values = append(values, value)
			}
		}
		return values, nil
	case "scalar":
		var sample []json.RawMessage
		if err := json.Unmarshal(envelope.Result, &sample); err != nil {
			return nil, fmt.Errorf("decode scalar result: %w", err)
		}
		value, ok, err := parsePrometheusValue(sample)
		if err != nil || !ok {
			return nil, err
		}
		return []float64{value}, nil
	case "matrix":
		var samples []struct {
			Values [][]json.RawMessage `json:"values"`
		}
		if err := json.Unmarshal(envelope.Result, &samples); err != nil {
			return nil, fmt.Errorf("decode matrix result: %w", err)
		}
		values := make([]float64, 0, len(samples))
		for _, sample := range samples {
			if len(sample.Values) == 0 {
				continue
			}
			value, ok, err := parsePrometheusValue(sample.Values[len(sample.Values)-1])
			if err != nil {
				return nil, err
			}
			if ok {
				values = append(values, value)
			}
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unsupported prometheus result type %q", envelope.ResultType)
	}
}

func parsePrometheusValue(raw []json.RawMessage) (float64, bool, error) {
	if len(raw) < 2 {
		return 0, false, nil
	}

	var text string
	if err := json.Unmarshal(raw[1], &text); err != nil {
		return 0, false, fmt.Errorf("decode sample value: %w", err)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse sample value %q: %w", text, err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false, nil
	}
	return value, true, nil
}

func firingMessage(rule store.AlertRule, value *float64) string {
	return fmt.Sprintf(":rotating_light: Monitoring Tool alert firing: %s\nSeverity: %s\nValue: %s %s %.4g\nQuery: `%s`",
		rule.Name,
		rule.Severity,
		formatValue(value),
		rule.Operator,
		rule.Threshold,
		rule.PromQL,
	)
}

func resolvedMessage(rule store.AlertRule, value *float64) string {
	return fmt.Sprintf(":white_check_mark: Monitoring Tool alert resolved: %s\nSeverity: %s\nCurrent value: %s\nQuery: `%s`",
		rule.Name,
		rule.Severity,
		formatValue(value),
		rule.PromQL,
	)
}

func formatValue(value *float64) string {
	if value == nil {
		return "no data"
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(*value, 'f', 4, 64), "0"), ".")
}
