package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"monitoring-tool/backend/internal/auth"
	promclient "monitoring-tool/backend/internal/prometheus"
)

const (
	minRefreshInterval = 2 * time.Second
	maxRefreshInterval = 5 * time.Minute
)

type Manager struct {
	prometheus *promclient.Client
	auth       *auth.Service
	logger     *slog.Logger

	mu      sync.Mutex
	queries map[string]*liveQuery
}

type liveQuery struct {
	key      string
	promql   string
	interval time.Duration
	cancel   context.CancelFunc

	subscribers map[*client]map[string]struct{}
}

type client struct {
	conn *websocket.Conn

	mu            sync.Mutex
	subscriptions map[string]map[string]struct{}
}

type incomingMessage struct {
	Type     string              `json:"type"`
	Panels   []panelSubscription `json:"panels"`
	PanelIDs []string            `json:"panel_ids"`
}

type panelSubscription struct {
	PanelID                string `json:"panel_id"`
	PromQL                 string `json:"promql"`
	RefreshIntervalSeconds int    `json:"refresh_interval_seconds"`
}

type outgoingMessage struct {
	Type      string          `json:"type"`
	PanelID   string          `json:"panel_id,omitempty"`
	Timestamp int64           `json:"timestamp,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Message   string          `json:"message,omitempty"`
}

func NewManager(prometheus *promclient.Client, authService *auth.Service, logger *slog.Logger) *Manager {
	return &Manager{
		prometheus: prometheus,
		auth:       authService,
		logger:     logger,
		queries:    map[string]*liveQuery{},
	}
}

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	if _, err := m.auth.Verify(token); err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		m.logger.Warn("websocket accept failed", "error", err)
		return
	}

	c := &client{conn: conn, subscriptions: map[string]map[string]struct{}{}}
	defer func() {
		m.removeClient(c)
		_ = conn.Close(websocket.StatusNormalClosure, "closed")
	}()

	ctx := r.Context()
	for {
		var msg incomingMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			status := websocket.CloseStatus(err)
			if status != websocket.StatusNormalClosure && status != websocket.StatusGoingAway && !errors.Is(err, context.Canceled) {
				m.logger.Debug("websocket read stopped", "error", err)
			}
			return
		}
		if err := m.handleMessage(ctx, c, msg); err != nil {
			_ = c.write(ctx, outgoingMessage{Type: "error", Message: err.Error()})
		}
	}
}

func (m *Manager) handleMessage(ctx context.Context, c *client, msg incomingMessage) error {
	switch msg.Type {
	case "subscribe":
		if len(msg.Panels) == 0 {
			return fmt.Errorf("subscribe requires panels")
		}
		for _, panel := range msg.Panels {
			if err := m.subscribe(c, panel); err != nil {
				return err
			}
		}
		return c.write(ctx, outgoingMessage{Type: "subscribed"})
	case "unsubscribe":
		if len(msg.PanelIDs) == 0 {
			return fmt.Errorf("unsubscribe requires panel_ids")
		}
		m.unsubscribePanels(c, msg.PanelIDs)
		return c.write(ctx, outgoingMessage{Type: "unsubscribed"})
	default:
		return fmt.Errorf("unsupported message type %q", msg.Type)
	}
}

func (m *Manager) subscribe(c *client, panel panelSubscription) error {
	panelID := strings.TrimSpace(panel.PanelID)
	promql := strings.TrimSpace(panel.PromQL)
	if panelID == "" || promql == "" {
		return fmt.Errorf("panel_id and promql are required")
	}

	interval := clampInterval(time.Duration(panel.RefreshIntervalSeconds) * time.Second)
	key := queryKey(promql, interval)

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := c.subscriptions[panelID]; ok {
		for oldKey := range existing {
			m.removeSubscriptionLocked(c, oldKey, panelID)
		}
	}

	query := m.queries[key]
	if query == nil {
		ctx, cancel := context.WithCancel(context.Background())
		query = &liveQuery{
			key:         key,
			promql:      promql,
			interval:    interval,
			cancel:      cancel,
			subscribers: map[*client]map[string]struct{}{},
		}
		m.queries[key] = query
		go m.runQuery(ctx, query)
	}

	if query.subscribers[c] == nil {
		query.subscribers[c] = map[string]struct{}{}
	}
	query.subscribers[c][panelID] = struct{}{}
	c.subscriptions[panelID] = map[string]struct{}{key: {}}
	return nil
}

func (m *Manager) unsubscribePanels(c *client, panelIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, panelID := range panelIDs {
		for key := range c.subscriptions[panelID] {
			m.removeSubscriptionLocked(c, key, panelID)
		}
		delete(c.subscriptions, panelID)
	}
}

func (m *Manager) removeClient(c *client) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for panelID, keys := range c.subscriptions {
		for key := range keys {
			m.removeSubscriptionLocked(c, key, panelID)
		}
	}
	c.subscriptions = map[string]map[string]struct{}{}
}

func (m *Manager) removeSubscriptionLocked(c *client, key, panelID string) {
	query := m.queries[key]
	if query == nil {
		return
	}
	delete(query.subscribers[c], panelID)
	if len(query.subscribers[c]) == 0 {
		delete(query.subscribers, c)
	}
	if len(query.subscribers) == 0 {
		query.cancel()
		delete(m.queries, key)
	}
}

func (m *Manager) runQuery(ctx context.Context, query *liveQuery) {
	m.pushQueryUpdate(ctx, query)

	ticker := time.NewTicker(query.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.pushQueryUpdate(ctx, query)
		}
	}
}

func (m *Manager) pushQueryUpdate(ctx context.Context, query *liveQuery) {
	queryCtx, cancel := context.WithTimeout(ctx, m.prometheus.Timeout())
	defer cancel()

	data, err := m.prometheus.InstantQuery(queryCtx, query.promql)
	m.mu.Lock()
	recipients := make(map[*client][]string, len(query.subscribers))
	for c, panelIDs := range query.subscribers {
		for panelID := range panelIDs {
			recipients[c] = append(recipients[c], panelID)
		}
		sort.Strings(recipients[c])
	}
	m.mu.Unlock()

	for c, panelIDs := range recipients {
		for _, panelID := range panelIDs {
			msg := outgoingMessage{Type: "metric_update", PanelID: panelID, Timestamp: time.Now().Unix()}
			if err != nil {
				msg.Type = "error"
				msg.Message = err.Error()
			} else {
				msg.Data = data
			}
			if writeErr := c.write(ctx, msg); writeErr != nil {
				m.logger.Debug("websocket write failed", "error", writeErr)
			}
		}
	}
}

func (c *client) write(ctx context.Context, msg outgoingMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return wsjson.Write(ctx, c.conn, msg)
}

func clampInterval(value time.Duration) time.Duration {
	if value < minRefreshInterval {
		return minRefreshInterval
	}
	if value > maxRefreshInterval {
		return maxRefreshInterval
	}
	return value
}

func queryKey(promql string, interval time.Duration) string {
	return strings.TrimSpace(promql) + "\x00" + interval.String()
}
