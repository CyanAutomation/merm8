package parser

import (
	"bytes"
	"testing"
)

func TestStderrWriterKeepsOnlyMostRecentBytes(t *testing.T) {
	w := &parserWorker{}
	writer := stderrWriter{worker: w}

	first := bytes.Repeat([]byte("a"), maxWorkerStderrBytes-10)
	second := bytes.Repeat([]byte("b"), 32)

	if _, err := writer.Write(first); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	if _, err := writer.Write(second); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}

	if got := len(w.stderr); got != maxWorkerStderrBytes {
		t.Fatalf("stderr length = %d, want %d", got, maxWorkerStderrBytes)
	}

	wantPrefix := bytes.Repeat([]byte("a"), maxWorkerStderrBytes-len(second))
	if !bytes.Equal(w.stderr[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("stderr prefix was not preserved as expected")
	}
	if !bytes.Equal(w.stderr[len(w.stderr)-len(second):], second) {
		t.Fatalf("stderr suffix does not contain most recent bytes")
	}
}

func TestStderrWriterLargeChunkReplacesBufferWithTail(t *testing.T) {
	w := &parserWorker{}
	writer := stderrWriter{worker: w}

	chunk := bytes.Repeat([]byte("z"), maxWorkerStderrBytes+123)
	if _, err := writer.Write(chunk); err != nil {
		t.Fatalf("write large chunk: %v", err)
	}

	if got := len(w.stderr); got != maxWorkerStderrBytes {
		t.Fatalf("stderr length = %d, want %d", got, maxWorkerStderrBytes)
	}
	want := chunk[len(chunk)-maxWorkerStderrBytes:]
	if !bytes.Equal(w.stderr, want) {
		t.Fatalf("stderr buffer does not match expected tail of large chunk")
	}
}
