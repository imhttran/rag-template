// PostgreSQL integration test for CheckEmbeddingDim.
//
// It is skipped unless RAG_INTEGRATION=1 is set, matching the conventions in
// integration_test.go, so `go test ./...` stays fast.
//
//	RAG_INTEGRATION=1 go test ./internal/ingestion/ -run IntegrationEmbeddingDim -v
package ingestion

import (
	"context"
	"strings"
	"testing"
)

// A fresh database migrated through migrations/ passes the guard at the default
// 768 dimension and fails with an actionable message at any other dimension.
func TestCheckEmbeddingDimIntegration(t *testing.T) {
	ctx := context.Background()
	requireDatabase(t)

	if err := CheckEmbeddingDim(ctx, testConn, 768); err != nil {
		t.Fatalf("CheckEmbeddingDim(768) = %v, want nil against the 001_init schema", err)
	}

	err := CheckEmbeddingDim(ctx, testConn, 1024)
	if err == nil {
		t.Fatal("CheckEmbeddingDim(1024) = nil, want a mismatch error")
	}

	for _, want := range []string{"1024", "768", migrationFile, "re-ingest"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to contain %q", err, want)
		}
	}
}
