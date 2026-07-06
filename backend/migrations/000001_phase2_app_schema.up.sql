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
