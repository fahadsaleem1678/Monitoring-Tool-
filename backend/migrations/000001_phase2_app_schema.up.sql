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

create table if not exists incident_reviews (
  id uuid primary key,
  alert_event_id uuid references alert_events(id),
  alert_rule_id uuid references alert_rules(id),
  status text not null check (status in ('pending_investigation', 'investigating', 'awaiting_review', 'approved', 'broadcasted', 'rejected', 'failed', 'closed')),
  severity text not null,
  title text not null,
  summary text not null default '',
  confidence text not null default '',
  draft_message text not null default '',
  final_message text not null default '',
  assigned_to uuid references users(id),
  approved_by uuid references users(id),
  approved_at timestamptz,
  broadcasted_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists incident_investigation_steps (
  id uuid primary key,
  incident_review_id uuid not null references incident_reviews(id) on delete cascade,
  step_type text not null,
  tool_name text not null default '',
  query_or_command text not null default '',
  result_summary text not null default '',
  raw_result_json jsonb not null default '{}',
  created_at timestamptz not null default now()
);

create table if not exists incident_audit_events (
  id uuid primary key,
  incident_review_id uuid not null references incident_reviews(id) on delete cascade,
  actor_type text not null,
  actor_id uuid references users(id),
  action text not null,
  details_json jsonb not null default '{}',
  created_at timestamptz not null default now()
);

alter table users enable row level security;
alter table dashboards enable row level security;
alter table panels enable row level security;
alter table alert_rules enable row level security;
alter table alert_events enable row level security;
alter table incident_reviews enable row level security;
alter table incident_investigation_steps enable row level security;
alter table incident_audit_events enable row level security;
