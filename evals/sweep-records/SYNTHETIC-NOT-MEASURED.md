# SYNTHETIC — NOT MEASURED

The JSON files in this directory are **synthetic fixtures**, not measurements.
They were produced by a unit test (`go test ./internal/retrieval/ -run TestSweepExperimentRecords`)
using `FixtureSearcher` in `internal/retrieval/sweep.go`, whose similarities are derived from a
SHA-256 of source/section/evidence — a stand-in for a real embedding cosine, NOT pgvector
retrieval. Case counts are invalid (`en-vi`/`vi-en` are 39/21 here but have 5 cases each in the
held-out split) and cross-lingual `rank1_rate=1.0` contradicts the measured 0.00.

**Do not cite these as evidence.** Real, operator-verified results are in
`docs/rq-003-embedding-model-matrix.md` and `docs/rq-024-operator-recovery-evidence.md`.
