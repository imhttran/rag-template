-- Bootstrap for the RAG document store: the pgvector extension, the documents
-- table, and the similarity index.
--
-- Applied automatically on first start by docker-compose (mounted into
-- /docker-entrypoint-initdb.d). To apply it manually:
--   psql 'postgres://rag:rag@127.0.0.1:5433/rag?sslmode=disable' -f migrations/001_init.sql

-- pgvector adds the `vector` type and the distance operators, e.g. `<=>`.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS documents (
    id          bigserial PRIMARY KEY,
    content     text NOT NULL,
    source      text,
    section     text,
    chunk_index integer NOT NULL DEFAULT 0,
    embedding   vector(768) NOT NULL
);

-- Approximate nearest-neighbour index for the `ORDER BY embedding <=> $1`
-- query in internal/retrieval. Optional for tiny corpora; it pays off as the
-- table grows. Cosine distance, matching the `<=>` operator.
CREATE INDEX IF NOT EXISTS documents_embedding_idx
    ON documents USING hnsw (embedding vector_cosine_ops);
