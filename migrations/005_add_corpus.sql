-- Adds a nullable `corpus` metadata column to documents so retrieval can be
-- scoped to a trusted corpus identity (RAG-SOP-002, architecture D2).
--
-- Backward compatible: the column is nullable with no default and no backfill,
-- so pre-existing rows read NULL. A NULL or empty corpus never matches a corpus
-- filter, so legacy rows are excluded from scoped retrieval until an explicit
-- operator backfill assigns a corpus.
--
-- Applied after 004_page.sql. The statement is idempotent (IF NOT EXISTS):
--     psql "$DATABASE_URL" -f migrations/005_add_corpus.sql

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS corpus text;

-- Supports the equality/allowlist lookup that scopes retrieval by corpus.
CREATE INDEX IF NOT EXISTS documents_corpus_idx ON documents (corpus);
