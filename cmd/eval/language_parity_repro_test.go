package main

import (
	"os"
	"strings"
	"testing"
)

// REGRESSION(F4): eval/rag language parity. cmd/rag threads
// config.Config.Language into retrieval.PipelineOptions.Language, and cmd/eval's
// evaluator.pipelineOptions() now does the same, so keyword retrieval uses the
// configured CORPUS_LANGUAGE full-text search configuration (empty keeps the
// baseline 'english'). This test is updated (RAG-023C) from the A3
// characterization baseline (which pinned eval's divergence) to assert parity.
func TestEvaluatorPipelineOptionsForwardsLanguage(t *testing.T) {
	tests := []struct {
		name     string
		language string
	}{
		{name: "empty keeps the baseline english config", language: ""},
		{name: "vi is forwarded", language: "vi"},
		{name: "english is forwarded", language: "english"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			eval := evaluator{language: test.language}

			options := eval.pipelineOptions()

			if options.Language != test.language {
				t.Fatalf(
					"evaluator.pipelineOptions().Language = %q, want %q (eval must forward cfg.Language)",
					options.Language,
					test.language,
				)
			}
		})
	}
}

// TestRagThreadsConfigLanguage is the rag side of the F4 parity pin. cmd/rag is
// package main and its pipeline-options construction is not reachable from this
// package's tests, so the wiring is pinned by reading the production source: it
// must still set PipelineOptions.Language from cfg.Language. Together with the
// eval-side assertion above, this pins language parity between the consumers.
func TestRagThreadsConfigLanguage(t *testing.T) {
	const ragMain = "../rag/main.go"

	source, err := os.ReadFile(ragMain)
	if err != nil {
		t.Fatalf("read %s: %v", ragMain, err)
	}

	// The rag side must still thread the configured language into the pipeline
	// options; if the wiring is removed, the parity assertion is no longer
	// meaningful and this test fails loudly.
	if !strings.Contains(string(source), "Language: cfg.Language") {
		t.Fatalf(
			"%s no longer sets PipelineOptions.Language from cfg.Language; the F4 parity baseline must be updated deliberately",
			ragMain,
		)
	}
}

// TestEvaluatorForwardsConfigLanguage pins the run wiring: the evaluator must
// carry cfg.Language from config.Load so pipelineOptions can forward it. The
// wiring is pinned by reading the production source so a future edit that drops
// the field is caught here.
func TestEvaluatorForwardsConfigLanguage(t *testing.T) {
	const evalMain = "main.go"

	source, err := os.ReadFile(evalMain)
	if err != nil {
		t.Fatalf("read %s: %v", evalMain, err)
	}

	// The evaluator struct literal must carry cfg.Language so pipelineOptions can
	// forward it; if the wiring is removed or renamed, this test fails loudly.
	if !strings.Contains(string(source), "language:          cfg.Language") {
		t.Fatalf("%s no longer wires cfg.Language into the evaluator", evalMain)
	}
}
