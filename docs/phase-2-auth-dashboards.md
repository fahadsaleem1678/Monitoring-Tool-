# Phase 2 Auth, Users, Dashboards, and Panels

Goal: turn the read-only MVP into a usable dashboard product.

## Implemented

- Users table with bcrypt password hashes.
- Admin seed user from environment variables.
- JWT login, logout, and current-user endpoints.
- Admin/viewer role shape.
- Auth middleware for saved dashboard APIs.
- Dashboard CRUD APIs.
- Panel create, update, delete APIs.
- Panel preview endpoint.
- Frontend login screen.
- Frontend dashboard list and dashboard detail view.
- Frontend add/delete saved panels.
- Saved panels render PromQL data through the Go backend.

## Default Local Admin

```text
username: admin
password: admin123

username: viewer
password: viewer123
```

Override these with:

```text
ADMIN_USERNAME=
ADMIN_PASSWORD=
VIEWER_USERNAME=
VIEWER_PASSWORD=
JWT_SECRET=
```

## Database

The backend runs the app schema automatically on startup using `DATABASE_URL`.
Migration files are also stored under `backend/migrations/`.

Run migrations without starting the backend:

```powershell
cd "E:\Internship\Monitoring tool"
.\scripts\migrate-supabase.ps1
```

The script prompts for the Supabase Postgres URI with hidden input, appends
`sslmode=require` when needed, runs the migration command in Docker, and removes
the connection string from the shell environment afterwards.

Supabase note: app tables enable RLS as defense in depth. The current backend is
expected to access Postgres through the server-side `DATABASE_URL`; the frontend
does not use Supabase keys or the Data API directly.

## Verified

- `go test ./...`
- `npm run build`
- Backend startup ran migrations against local Postgres.
- Login with seeded admin succeeded.
- Dashboard create succeeded.
- Panel create succeeded.
- Dashboard fetch returned the saved panel.
- Panel preview returned live Prometheus vector data.
- Phase 2 frontend served on `http://localhost:5174` and proxied login through the backend.
