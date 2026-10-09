-- Adds a nullable `language` metadata column to documents so retrieval can pick
-- a language-appropriate full-text search configuration.
--
-- Applied after 002_ingestion_metadata.sql. Every statement is idempotent
-- (IF NOT EXISTS), so re-running it against a database already at 003 is a
-- no-op.
--   psql 'postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable' \
--     -f migrations/003_add_language.sql

-- language records the document language as a BCP-47 tag (for example "en",
-- "de", "fr"). It is nullable with NO server-side default, so pre-existing rows
-- read as NULL (unset); retrieval treats NULL as the current baseline
-- configuration ('english'), so no default behavior changes.
ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS language text;
