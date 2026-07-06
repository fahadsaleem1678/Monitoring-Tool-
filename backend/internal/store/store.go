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
