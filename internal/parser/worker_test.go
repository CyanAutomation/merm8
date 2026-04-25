package parser

import (
	"bytes"
	"testing"
)

func TestStderrWriterWriteReturnsOriginalLengthOnTruncation(t *testing.T) {
	initial := bytes.Repeat([]byte{'a'}, maxWorkerStderrBytes-4)
	w := &parserWorker{stderr: append([]byte{}, initial...)}
	sw := stderrWriter{worker: w}

	input := []byte("0123456789")
	n, err := sw.Write(input)
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if n != len(input) {
		t.Fatalf("Write() n = %d, want %d", n, len(input))
	}

	if len(w.stderr) != maxWorkerStderrBytes {
		t.Fatalf("len(stderr) = %d, want %d", len(w.stderr), maxWorkerStderrBytes)
	}
	wantTail := []byte("6789")
	gotTail := w.stderr[len(w.stderr)-len(wantTail):]
	if !bytes.Equal(gotTail, wantTail) {
		t.Fatalf("stderr tail = %q, want %q", gotTail, wantTail)
	}
}
