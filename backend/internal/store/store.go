package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Store struct {
	pool *pgxpool.Pool
}

type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

type Dashboard struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	OwnerID     uuid.UUID `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Panels      []Panel   `json:"panels,omitempty"`
}

type Panel struct {
	ID                     uuid.UUID      `json:"id"`
	DashboardID            uuid.UUID      `json:"dashboard_id"`
	Title                  string         `json:"title"`
	PromQL                 string         `json:"promql"`
	VisualizationType      string         `json:"visualization_type"`
	GridX                  int            `json:"grid_x"`
	GridY                  int            `json:"grid_y"`
	GridW                  int            `json:"grid_w"`
	GridH                  int            `json:"grid_h"`
	RefreshIntervalSeconds int            `json:"refresh_interval_seconds"`
	SettingsJSON           map[string]any `json:"settings_json"`
}

type AlertRule struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	PromQL     string    `json:"promql"`
	Operator   string    `json:"operator"`
	Threshold  float64   `json:"threshold"`
	ForSeconds int       `json:"for_seconds"`
	Severity   string    `json:"severity"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type AlertEvent struct {
	ID         uuid.UUID  `json:"id"`
	RuleID     *uuid.UUID `json:"rule_id,omitempty"`
	Status     string     `json:"status"`
	Value      *float64   `json:"value,omitempty"`
	Message    string     `json:"message"`
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, schemaSQL)
	return err
}

func (s *Store) EnsureAdmin(ctx context.Context, username, password string) error {
	return s.EnsureUser(ctx, username, password, "admin")
}

func (s *Store) EnsureUser(ctx context.Context, username, password, role string) error {
	if err := ValidateRole(role); err != nil {
		return err
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `select exists(select 1 from users where username = $1)`, username).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		insert into users (id, username, password_hash, role, created_at)
		values ($1, $2, $3, $4, now())
	`, uuid.New(), username, string(hash), role)
	return err
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		select id, username, password_hash, role, created_at
		from users
		where username = $1
	`, username).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.CreatedAt)
	return user, err
}

func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		select id, username, password_hash, role, created_at
		from users
		where id = $1
	`, id).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.CreatedAt)
	return user, err
}

func (s *Store) ListDashboards(ctx context.Context) ([]Dashboard, error) {
	rows, err := s.pool.Query(ctx, `
		select id, title, coalesce(description, ''), owner_id, created_at, updated_at
		from dashboards
		order by updated_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dashboards []Dashboard
	for rows.Next() {
		var dashboard Dashboard
		if err := rows.Scan(&dashboard.ID, &dashboard.Title, &dashboard.Description, &dashboard.OwnerID, &dashboard.CreatedAt, &dashboard.UpdatedAt); err != nil {
			return nil, err
		}
		dashboards = append(dashboards, dashboard)
	}
	return dashboards, rows.Err()
}

func (s *Store) DashboardByID(ctx context.Context, id uuid.UUID) (Dashboard, error) {
	var dashboard Dashboard
	err := s.pool.QueryRow(ctx, `
		select id, title, coalesce(description, ''), owner_id, created_at, updated_at
		from dashboards
		where id = $1
	`, id).Scan(&dashboard.ID, &dashboard.Title, &dashboard.Description, &dashboard.OwnerID, &dashboard.CreatedAt, &dashboard.UpdatedAt)
	if err != nil {
		return Dashboard{}, err
	}

	panels, err := s.ListPanels(ctx, id)
	if err != nil {
		return Dashboard{}, err
	}
	dashboard.Panels = panels
	return dashboard, nil
}

func (s *Store) CreateDashboard(ctx context.Context, title, description string, ownerID uuid.UUID) (Dashboard, error) {
	id := uuid.New()
	var dashboard Dashboard
	err := s.pool.QueryRow(ctx, `
		insert into dashboards (id, title, description, owner_id, created_at, updated_at)
		values ($1, $2, $3, $4, now(), now())
		returning id, title, coalesce(description, ''), owner_id, created_at, updated_at
	`, id, title, description, ownerID).Scan(&dashboard.ID, &dashboard.Title, &dashboard.Description, &dashboard.OwnerID, &dashboard.CreatedAt, &dashboard.UpdatedAt)
	return dashboard, err
}

func (s *Store) UpdateDashboard(ctx context.Context, id uuid.UUID, title, description string) (Dashboard, error) {
	var dashboard Dashboard
	err := s.pool.QueryRow(ctx, `
		update dashboards
		set title = $2, description = $3, updated_at = now()
		where id = $1
		returning id, title, coalesce(description, ''), owner_id, created_at, updated_at
	`, id, title, description).Scan(&dashboard.ID, &dashboard.Title, &dashboard.Description, &dashboard.OwnerID, &dashboard.CreatedAt, &dashboard.UpdatedAt)
	return dashboard, err
}

func (s *Store) DeleteDashboard(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `delete from dashboards where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Store) ListPanels(ctx context.Context, dashboardID uuid.UUID) ([]Panel, error) {
	rows, err := s.pool.Query(ctx, `
		select id, dashboard_id, title, promql, visualization_type, grid_x, grid_y, grid_w, grid_h,
		       refresh_interval_seconds, settings_json
		from panels
		where dashboard_id = $1
		order by grid_y, grid_x, title
	`, dashboardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var panels []Panel
	for rows.Next() {
		var panel Panel
		if err := rows.Scan(&panel.ID, &panel.DashboardID, &panel.Title, &panel.PromQL, &panel.VisualizationType, &panel.GridX, &panel.GridY, &panel.GridW, &panel.GridH, &panel.RefreshIntervalSeconds, &panel.SettingsJSON); err != nil {
			return nil, err
		}
		panels = append(panels, panel)
	}
	return panels, rows.Err()
}

func (s *Store) CreatePanel(ctx context.Context, panel Panel) (Panel, error) {
	panel.ID = uuid.New()
	if panel.VisualizationType == "" {
		panel.VisualizationType = "line"
	}
	if panel.RefreshIntervalSeconds == 0 {
		panel.RefreshIntervalSeconds = 30
	}
	if panel.SettingsJSON == nil {
		panel.SettingsJSON = map[string]any{}
	}

	err := s.pool.QueryRow(ctx, `
		insert into panels (id, dashboard_id, title, promql, visualization_type, grid_x, grid_y, grid_w, grid_h, refresh_interval_seconds, settings_json)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		returning id, dashboard_id, title, promql, visualization_type, grid_x, grid_y, grid_w, grid_h, refresh_interval_seconds, settings_json
	`, panel.ID, panel.DashboardID, panel.Title, panel.PromQL, panel.VisualizationType, panel.GridX, panel.GridY, panel.GridW, panel.GridH, panel.RefreshIntervalSeconds, panel.SettingsJSON).Scan(
		&panel.ID, &panel.DashboardID, &panel.Title, &panel.PromQL, &panel.VisualizationType, &panel.GridX, &panel.GridY, &panel.GridW, &panel.GridH, &panel.RefreshIntervalSeconds, &panel.SettingsJSON,
	)
	if err != nil {
		return Panel{}, err
	}
	_, _ = s.pool.Exec(ctx, `update dashboards set updated_at = now() where id = $1`, panel.DashboardID)
	return panel, nil
}

func (s *Store) UpdatePanel(ctx context.Context, panel Panel) (Panel, error) {
	if panel.SettingsJSON == nil {
		panel.SettingsJSON = map[string]any{}
	}

	err := s.pool.QueryRow(ctx, `
		update panels
		set title = $2, promql = $3, visualization_type = $4, grid_x = $5, grid_y = $6,
		    grid_w = $7, grid_h = $8, refresh_interval_seconds = $9, settings_json = $10
		where id = $1
		returning id, dashboard_id, title, promql, visualization_type, grid_x, grid_y, grid_w, grid_h, refresh_interval_seconds, settings_json
	`, panel.ID, panel.Title, panel.PromQL, panel.VisualizationType, panel.GridX, panel.GridY, panel.GridW, panel.GridH, panel.RefreshIntervalSeconds, panel.SettingsJSON).Scan(
		&panel.ID, &panel.DashboardID, &panel.Title, &panel.PromQL, &panel.VisualizationType, &panel.GridX, &panel.GridY, &panel.GridW, &panel.GridH, &panel.RefreshIntervalSeconds, &panel.SettingsJSON,
	)
	if err != nil {
		return Panel{}, err
	}
	_, _ = s.pool.Exec(ctx, `update dashboards set updated_at = now() where id = $1`, panel.DashboardID)
	return panel, nil
}

func (s *Store) DeletePanel(ctx context.Context, id uuid.UUID) error {
	var dashboardID uuid.UUID
	err := s.pool.QueryRow(ctx, `delete from panels where id = $1 returning dashboard_id`, id).Scan(&dashboardID)
	if err != nil {
		return err
	}
	_, _ = s.pool.Exec(ctx, `update dashboards set updated_at = now() where id = $1`, dashboardID)
	return nil
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.pool.Query(ctx, `
		select id, name, promql, operator, threshold, for_seconds, severity, enabled, created_at, updated_at
		from alert_rules
		order by updated_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []AlertRule{}
	for rows.Next() {
		var rule AlertRule
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.PromQL, &rule.Operator, &rule.Threshold, &rule.ForSeconds, &rule.Severity, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Store) OpenAlertEventByRuleID(ctx context.Context, ruleID uuid.UUID) (AlertEvent, bool, error) {
	var event AlertEvent
	var ruleIDText string
	err := s.pool.QueryRow(ctx, `
		select id, coalesce(rule_id::text, ''), status, value, coalesce(message, ''), started_at, resolved_at
		from alert_events
		where rule_id = $1 and status = 'firing' and resolved_at is null
		order by started_at desc
		limit 1
	`, ruleID).Scan(&event.ID, &ruleIDText, &event.Status, &event.Value, &event.Message, &event.StartedAt, &event.ResolvedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return AlertEvent{}, false, nil
		}
		return AlertEvent{}, false, err
	}
	if ruleIDText != "" {
		parsedRuleID, err := uuid.Parse(ruleIDText)
		if err != nil {
			return AlertEvent{}, false, err
		}
		event.RuleID = &parsedRuleID
	}
	return event, true, nil
}

func (s *Store) CreateAlertRule(ctx context.Context, rule AlertRule) (AlertRule, error) {
	rule.ID = uuid.New()
	if rule.ForSeconds <= 0 {
		rule.ForSeconds = 60
	}
	if rule.Severity == "" {
		rule.Severity = "warning"
	}

	err := s.pool.QueryRow(ctx, `
		insert into alert_rules (id, name, promql, operator, threshold, for_seconds, severity, enabled, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, now(), now())
		returning id, name, promql, operator, threshold, for_seconds, severity, enabled, created_at, updated_at
	`, rule.ID, rule.Name, rule.PromQL, rule.Operator, rule.Threshold, rule.ForSeconds, rule.Severity, rule.Enabled).Scan(
		&rule.ID, &rule.Name, &rule.PromQL, &rule.Operator, &rule.Threshold, &rule.ForSeconds, &rule.Severity, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
	)
	return rule, err
}

func (s *Store) UpdateAlertRule(ctx context.Context, rule AlertRule) (AlertRule, error) {
	err := s.pool.QueryRow(ctx, `
		update alert_rules
		set name = $2, promql = $3, operator = $4, threshold = $5, for_seconds = $6,
		    severity = $7, enabled = $8, updated_at = now()
		where id = $1
		returning id, name, promql, operator, threshold, for_seconds, severity, enabled, created_at, updated_at
	`, rule.ID, rule.Name, rule.PromQL, rule.Operator, rule.Threshold, rule.ForSeconds, rule.Severity, rule.Enabled).Scan(
		&rule.ID, &rule.Name, &rule.PromQL, &rule.Operator, &rule.Threshold, &rule.ForSeconds, &rule.Severity, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
	)
	return rule, err
}

func (s *Store) DeleteAlertRule(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `update alert_events set rule_id = null where rule_id = $1`, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `delete from alert_rules where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return tx.Commit(ctx)
}

func (s *Store) ListAlertEvents(ctx context.Context, limit int) ([]AlertEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		select id, coalesce(rule_id::text, ''), status, value, coalesce(message, ''), started_at, resolved_at
		from alert_events
		order by started_at desc
		limit $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []AlertEvent{}
	for rows.Next() {
		var event AlertEvent
		var ruleIDText string
		if err := rows.Scan(&event.ID, &ruleIDText, &event.Status, &event.Value, &event.Message, &event.StartedAt, &event.ResolvedAt); err != nil {
			return nil, err
		}
		if ruleIDText != "" {
			ruleID, err := uuid.Parse(ruleIDText)
			if err != nil {
				return nil, err
			}
			event.RuleID = &ruleID
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) CreateAlertEvent(ctx context.Context, event AlertEvent) (AlertEvent, error) {
	event.ID = uuid.New()
	var ruleIDText string
	err := s.pool.QueryRow(ctx, `
		insert into alert_events (id, rule_id, status, value, message, started_at, resolved_at)
		values ($1, $2, $3, $4, $5, now(), $6)
		returning id, coalesce(rule_id::text, ''), status, value, coalesce(message, ''), started_at, resolved_at
	`, event.ID, event.RuleID, event.Status, event.Value, event.Message, event.ResolvedAt).Scan(
		&event.ID, &ruleIDText, &event.Status, &event.Value, &event.Message, &event.StartedAt, &event.ResolvedAt,
	)
	if err != nil {
		return AlertEvent{}, err
	}
	if ruleIDText != "" {
		ruleID, err := uuid.Parse(ruleIDText)
		if err != nil {
			return AlertEvent{}, err
		}
		event.RuleID = &ruleID
	}
	return event, nil
}

func (s *Store) ResolveAlertEvent(ctx context.Context, id uuid.UUID, value *float64, message string) (AlertEvent, error) {
	var event AlertEvent
	var ruleIDText string
	err := s.pool.QueryRow(ctx, `
		update alert_events
		set status = 'resolved', value = $2, message = $3, resolved_at = now()
		where id = $1
		returning id, coalesce(rule_id::text, ''), status, value, coalesce(message, ''), started_at, resolved_at
	`, id, value, message).Scan(
		&event.ID, &ruleIDText, &event.Status, &event.Value, &event.Message, &event.StartedAt, &event.ResolvedAt,
	)
	if err != nil {
		return AlertEvent{}, err
	}
	if ruleIDText != "" {
		ruleID, err := uuid.Parse(ruleIDText)
		if err != nil {
			return AlertEvent{}, err
		}
		event.RuleID = &ruleID
	}
	return event, nil
}

func NotFound(err error) bool {
	return err == pgx.ErrNoRows
}

func ValidateRole(role string) error {
	if role != "admin" && role != "viewer" {
		return fmt.Errorf("invalid role %q", role)
	}
	return nil
}

const schemaSQL = `
create table if not exists users (
  id uuid primary key,
  username text unique not null,
  password_hash text not null,
  role text not null check (role in ('admin', 'viewer')),
  created_at timestamptz not null default now()
);

create table if not exists dashboards (
  id uuid primary key,
  title text not null,
  description text,
  owner_id uuid not null references users(id),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists panels (
  id uuid primary key,
  dashboard_id uuid not null references dashboards(id) on delete cascade,
  title text not null,
  promql text not null,
  visualization_type text not null default 'line',
  grid_x int not null default 0,
  grid_y int not null default 0,
  grid_w int not null default 6,
  grid_h int not null default 4,
  refresh_interval_seconds int not null default 30,
  settings_json jsonb not null default '{}'
);

create table if not exists alert_rules (
  id uuid primary key,
  name text not null,
  promql text not null,
  operator text not null,
  threshold double precision not null,
  for_seconds int not null,
  severity text not null,
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists alert_events (
  id uuid primary key,
  rule_id uuid references alert_rules(id),
  status text not null,
  value double precision,
  message text,
  started_at timestamptz not null default now(),
  resolved_at timestamptz
);

alter table users enable row level security;
alter table dashboards enable row level security;
alter table panels enable row level security;
alter table alert_rules enable row level security;
alter table alert_events enable row level security;
`
