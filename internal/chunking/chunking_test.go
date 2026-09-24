package chunking

import (
	"testing"
)

func TestSplitWordsWithOverlap(t *testing.T) {
	content := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen"

	chunks := splitWords(content, 10, 3)

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	expectedFirst :=
		"one two three four five six seven eight nine ten"

	expectedSecond :=
		"eight nine ten eleven twelve thirteen fourteen fifteen"

	if chunks[0] != expectedFirst {
		t.Errorf(
			"first chunk:\nexpected: %q\ngot:      %q",
			expectedFirst,
			chunks[0],
		)
	}

	if chunks[1] != expectedSecond {
		t.Errorf(
			"second chunk:\nexpected: %q\ngot:      %q",
			expectedSecond,
			chunks[1],
		)
	}
}
