# Migrations

MiniWiki currently runs lightweight embedded migrations from `internal/storage/storage.go`.
The `schema_migrations` table records which migration statements have been applied.

This folder is reserved for a future move to file-based migrations.
