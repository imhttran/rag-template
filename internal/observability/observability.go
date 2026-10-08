// Package observability provides a small, per-invocation reporter for the rag
// command. It mints one run ID per invocation and emits stage timings,
// retrieved/filtered counts, and context (token-estimate) usage under that ID.
//
// Two implementations exist:
//
//   - HumanReporter preserves the existing human-readable stage output: its
//     stage methods write nothing to stdout, so the default workflow is
//     byte-for-byte unchanged.
//   - JSONReporter emits one structured log/slog record (JSON handler) keyed by
//     the run ID, carrying all stage timings, counts, and context usage.
//
// The reporter has no external APM dependency; JSON mode uses the standard
// library log/slog (Go 1.21+).
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"time"
)

// Stage names reported per run. They map 1:1 to the pipeline boundaries in
// cmd/rag: embed, rewrite, retrieve, budget, the answerability gate, and the
// answer call.
const (
	StageEmbed         = "embed"
	StageRewrite       = "rewrite"
	StageRetrieve      = "retrieve"
	StageBudget        = "budget"
	StageAnswerability = "answerability"
	StageAnswer        = "answer"
)

// Counts holds the retrieved/filtered document tally for a run. Retrieved is
// the number of documents kept (similarity at or above the floor); Filtered is
// the number dropped by the similarity floor.
type Counts struct {
	Retrieved int `json:"retrieved"`
	Filtered  int `json:"filtered"`
}

// Usage holds the context/token estimate for a run. It is the RAG-010
// provider-neutral byte estimate over the documents actually sent to the
// model.
type Usage struct {
	ContextBytes int `json:"context_bytes"`
	Chunks       int `json:"chunks"`
}

// Record is the correlated observability record for one invocation. All fields
// are keyed by the shared RunID so a single run can be reconstructed from its
// JSON output. Stages holds each stage's wall-clock duration in milliseconds
// (the unit the stage_ms JSON tag names).
type Record struct {
	RunID  string             `json:"run_id"`
	Stages map[string]float64 `json:"stage_ms"`
	Counts Counts             `json:"counts"`
	Usage  Usage              `json:"usage"`
}

// Reporter receives per-run observability events. The human implementation
// records them without adding lines to the existing stream; the JSON
// implementation emits them as structured slog records.
type Reporter interface {
	// RunID returns the shared identifier minted for this invocation.
	RunID() string

	// Stage records the wall-clock duration of a named stage. The duration is
	// stored in milliseconds (the unit the stage_ms record field names), so
	// callers pass the duration directly and cannot confuse seconds with
	// milliseconds.
	Stage(name string, duration time.Duration)

	// Counts records the retrieved/filtered document tally.
	Counts(c Counts)

	// Usage records the context/token estimate for the documents sent.
	Usage(u Usage)

	// Finish flushes any buffered record. It is safe to call once at the end
	// of a run.
	Finish()
}

// NewRunID returns a fresh, unique, URL-safe run identifier. It never panics on
// an unexpected entropy failure: it falls back to a fixed-length sentinel so the
// reporting path cannot take down a run.
func NewRunID() string {
	var buf [8]byte

	if _, err := rand.Read(buf[:]); err != nil {
		return "run-unknown"
	}

	return "run-" + hex.EncodeToString(buf[:])
}

// HumanReporter is the default reporter. It adds no lines to the existing
// human-readable stream: Stage, Counts, and Usage are recorded (so tests can
// assert on them) but nothing is written to stdout. This keeps the default
// workflow byte-for-byte identical to the pre-RAG-014 output.
type HumanReporter struct {
	runID string

	record Record
}

// NewHumanReporter returns the default reporter: existing output is untouched.
func NewHumanReporter() *HumanReporter {
	return &HumanReporter{
		runID: NewRunID(),
		record: Record{
			Stages: make(map[string]float64),
		},
	}
}

// RunID returns the shared identifier for this invocation.
func (r *HumanReporter) RunID() string { return r.runID }

// Stage records a stage duration in milliseconds. It writes nothing to stdout.
func (r *HumanReporter) Stage(name string, duration time.Duration) {
	r.record.Stages[name] = float64(duration) / float64(time.Millisecond)
}

// Counts records the retrieved/filtered tally. It writes nothing to stdout.
func (r *HumanReporter) Counts(c Counts) { r.record.Counts = c }

// Usage records the context estimate. It writes nothing to stdout.
func (r *HumanReporter) Usage(u Usage) { r.record.Usage = u }

// Finish is a no-op for the human reporter: nothing was buffered for stdout.
func (r *HumanReporter) Finish() {}

// JSONReporter emits one correlated structured record per run through log/slog
// with a JSON handler. It requires Go 1.21+ (log/slog); the module targets Go
// 1.26, so the standard library is used directly. No external APM dependency is
// introduced.
type JSONReporter struct {
	runID  string
	logger *slog.Logger

	record Record
}

// NewJSONReporter returns a reporter that writes a single JSON record to w when
// Finish is called.
func NewJSONReporter(w io.Writer) *JSONReporter {
	logger := slog.New(slog.NewJSONHandler(w, nil))

	return &JSONReporter{
		runID:  NewRunID(),
		logger: logger,
		record: Record{
			Stages: make(map[string]float64),
		},
	}
}

// RunID returns the shared identifier for this invocation.
func (r *JSONReporter) RunID() string { return r.runID }

// Stage records a stage duration in milliseconds.
func (r *JSONReporter) Stage(name string, duration time.Duration) {
	r.record.Stages[name] = float64(duration) / float64(time.Millisecond)
}

// Counts records the retrieved/filtered tally.
func (r *JSONReporter) Counts(c Counts) { r.record.Counts = c }

// Usage records the context estimate.
func (r *JSONReporter) Usage(u Usage) { r.record.Usage = u }

// Finish emits the buffered record as a single slog JSON line keyed by the run
// ID. The stage timings (in milliseconds) are flattened into individual numeric
// attributes as well as the counts and usage fields, so a single shared run ID
// correlates every field.
func (r *JSONReporter) Finish() {
	r.record.RunID = r.runID

	attrs := []slog.Attr{
		slog.String("run_id", r.runID),
		slog.Int("retrieved", r.record.Counts.Retrieved),
		slog.Int("filtered", r.record.Counts.Filtered),
		slog.Int("context_bytes", r.record.Usage.ContextBytes),
		slog.Int("chunks", r.record.Usage.Chunks),
	}

	for name, millis := range r.record.Stages {
		attrs = append(attrs, slog.Float64("stage_ms_"+name, millis))
	}

	r.logger.LogAttrs(context.Background(), slog.LevelInfo, "run", attrs...)
}

// New returns the reporter selected by the configuration. useJSON selects the
// opt-in structured mode; otherwise the human reporter (which adds no lines to
// the existing stream) is returned.
func New(useJSON bool, w io.Writer) Reporter {
	if useJSON {
		return NewJSONReporter(w)
	}

	return NewHumanReporter()
}
