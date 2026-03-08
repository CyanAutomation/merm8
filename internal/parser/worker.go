package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

const maxWorkerStderrBytes = 64 * 1024

type workerRequestEnvelope struct {
	ID      string        `json:"id"`
	Code    string        `json:"code"`
	Timeout int64         `json:"timeout_ms"`
	Limits  *workerLimits `json:"limits,omitempty"`
}

type workerLimits struct {
	NodeMaxOldSpaceMB int `json:"node_max_old_space_mb,omitempty"`
}

type workerResponseEnvelope struct {
	ID     string      `json:"id"`
	Result ParseResult `json:"result"`
	Error  string      `json:"error,omitempty"`
}

type parserWorker struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	errMu    sync.Mutex
	stderr   []byte
	opMu     sync.Mutex
	closeMu  sync.Mutex
	isClosed bool
}

func startParserWorker(scriptPath string, nodeArgs []string) (*parserWorker, error) {
	args := append([]string{}, nodeArgs...)
	args = append(args, scriptPath, "--worker")
	cmd := exec.Command("node", args...) //nolint:gosec

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
		return nil, err
	}

	w := &parserWorker{stdin: stdin, stdout: bufio.NewReader(stdoutPipe), cmd: cmd}
	go func() {
		_, _ = io.Copy(stderrWriter{worker: w}, stderrPipe)
	}()

	return w, nil
}

func (w *parserWorker) do(req workerRequestEnvelope) (*workerResponseEnvelope, error) {
	w.opMu.Lock()
	defer w.opMu.Unlock()

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to encode parser request: %w", ErrDecode, err)
	}
	if _, err := w.stdin.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("%w: %w (stderr: %s)", ErrSubprocess, err, w.stderrString())
	}

	line, err := w.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("%w: %w (stderr: %s)", ErrSubprocess, err, w.stderrString())
	}

	var resp workerResponseEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
		return nil, fmt.Errorf("%w: failed to decode parser output: %w", ErrDecode, err)
	}
	if resp.ID != req.ID {
		return nil, fmt.Errorf("%w: mismatched worker response id", ErrContract)
	}

	return &resp, nil
}

func (w *parserWorker) close() {
	w.closeMu.Lock()
	defer w.closeMu.Unlock()
	if w.isClosed {
		return
	}
	w.isClosed = true
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
	_ = w.stdin.Close()
	_ = w.cmd.Wait()
}

func (w *parserWorker) stderrString() string {
	w.errMu.Lock()
	defer w.errMu.Unlock()
	return string(w.stderr)
}

type stderrWriter struct {
	worker *parserWorker
}

func (sw stderrWriter) Write(p []byte) (int, error) {
	sw.worker.errMu.Lock()
	defer sw.worker.errMu.Unlock()
	sw.worker.stderr = append(sw.worker.stderr, p...)
	if len(sw.worker.stderr) > maxWorkerStderrBytes {
		sw.worker.stderr = append([]byte(nil), sw.worker.stderr[len(sw.worker.stderr)-maxWorkerStderrBytes:]...)
	}
	return len(p), nil
}
