package parser

import (
	"bytes"
	"strings"
	"testing"
)

func TestStderrWriterCapsBuffer(t *testing.T) {
	w := &parserWorker{}
	writer := stderrWriter{worker: w}

	chunk := bytes.Repeat([]byte("x"), maxWorkerStderrBytes/2)
	_, _ = writer.Write(chunk)
	_, _ = writer.Write(chunk)
	_, _ = writer.Write([]byte("tail"))

	if got := len(w.stderr); got != maxWorkerStderrBytes {
		t.Fatalf("expected capped stderr length %d, got %d", maxWorkerStderrBytes, got)
	}

	if !strings.HasSuffix(w.stderrString(), "tail") {
		t.Fatalf("expected stderr ring buffer to retain latest output suffix")
	}
}
