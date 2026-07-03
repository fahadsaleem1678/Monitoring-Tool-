# Backend Migrations

This folder is reserved for PostgreSQL migrations. Phase 0 keeps the database
schema empty while the project foundation is verified.

Use migration filenames compatible with common migration tools:

```text
000001_create_users.up.sql
000001_create_users.down.sql
```

The application database is for product data only. Prometheus remains the
source of truth for metrics.
