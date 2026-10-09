-- Ingestion provenance: per-chunk metadata so "is this document up to date?"
-- is answerable from stored rows and unchanged files are not re-embedded.
--
-- Applied after 001_init.sql. Every statement is idempotent (IF NOT EXISTS),
-- so re-running it against a database already at 002 is a no-op.
--   psql 'postgres://rag:rag@127.0.0.1:5434/rag?sslmode=disable' \
--     -f migrations/002_ingestion_metadata.sql

-- content_hash is a fingerprint of the ingested file's bytes combined with the
-- chunker configuration, so a content or chunker-config change invalidates it.
ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS content_hash text;

-- embed_model and embedding_dim record which model produced embedding and how
-- wide it is, so a model or dimension change forces a re-embed.
ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS embed_model text;

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS embedding_dim integer;

-- chunker_config is a canonical serialization of the chunker settings
-- (size/overlap) used to produce the chunking.
ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS chunker_config text;

-- ingested_at records when the chunk was last written.
ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS ingested_at timestamptz NOT NULL DEFAULT now();

-- Supports the up-to-date lookup by source without scanning the whole table.
CREATE INDEX IF NOT EXISTS documents_source_idx ON documents (source);
