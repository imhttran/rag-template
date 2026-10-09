-- Adds a nullable `page` metadata column to documents so PDF pages carry a
-- 1-based page number from extraction through retrieval and citation
-- rendering.
--
-- Applied after 003_add_language.sql. The statement is idempotent
-- (IF NOT EXISTS), so re-running it against a database already at 004 is a
-- no-op.
--   psql 'postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable' \
--     -f migrations/004_page.sql

-- page records the 1-based page number a chunk came from (PDF loaders set
-- one Section per page). It is nullable with NO server-side default and no
-- backfill, so pre-existing rows and page-less formats (Markdown, plain text)
-- read as NULL; retrieval normalizes NULL to 0 and rendering omits the page
-- line, keeping those formats byte-identical to the pre-RAG-017 behavior.
ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS page integer;
