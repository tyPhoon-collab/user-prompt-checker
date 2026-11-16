package hook

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"user-prompt-checker/internal/checker"
)

func TestTruncateContentKeepsHeadAndTail(t *testing.T) {
	long := strings.Repeat("a", 250) + strings.Repeat("b", 500)
	got := TruncateContent(long, 600)
	if !strings.HasPrefix(got, strings.Repeat("a", 200)) {
		t.Fatalf("expected head preserved, got prefix: %q", got[:20])
	}
	if !strings.Contains(got, "\n...\n") {
		t.Fatal("expected ellipsis separator")
	}
	if !strings.HasSuffix(got, strings.Repeat("b", 400)) {
		t.Fatalf("expected tail preserved, got suffix: %q", got[len(got)-20:])
	}
}

func TestAppendEditedFilesDeduplicatesAndCaps(t *testing.T) {
	files := []string{"a.go", "b.go", "a.go", "c.go", "d.go", "e.go", "f.go"}
	got := AppendEditedFiles("Done.", files)
	want := "Done.\n[Edited files: a.go, b.go, c.go, d.go, e.go, ...]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTrimHistoryCapsAtMaxMessages(t *testing.T) {
	msgs := make([]checker.Message, 12)
	for i := range msgs {
		msgs[i] = checker.Message{Role: "user", Content: string(rune('a' + i))}
	}
	want := msgs[2:]
	if diff := cmp.Diff(want, TrimHistory(msgs)); diff != "" {
		t.Fatalf("trim mismatch (-want +got):\n%s", diff)
	}
}
