package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

var errCLITransportUnavailable = errors.New("claude cli transport unavailable")

type cliSession struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stderrBuf  bytes.Buffer
	stderrDone chan struct{}
	events     chan cliStreamMessage
	readDone   chan struct{}
	waitDone   chan struct{}
	doneCh     chan struct{}
	readErr    error
	waitErr    error
	closed     bool
	exited     bool

	controlMu       sync.Mutex
	pendingControls map[string]context.CancelFunc
}

func (a *Agent) ensureCLISession(ctx context.Context) error {
	a.cliMu.Lock()
	session := a.cliSession
	if session != nil && session.isOperational() {
		a.cliMu.Unlock()
		return nil
	}
	a.cliSession = nil
	a.cliMu.Unlock()

	if session != nil {
		_ = session.close()
	}

	session, err := a.startCLISession(ctx)
	if err != nil {
		return err
	}

	a.cliMu.Lock()
	a.cliSession = session
	a.cliMu.Unlock()
	return nil
}

func (a *Agent) startCLISession(ctx context.Context) (*cliSession, error) {
	if strings.TrimSpace(a.opts.CLICommand) == "" {
		return nil, fmt.Errorf("claude-cli provider requires CLICommand")
	}

	cmd, err := a.newCLICommand(ctx)
	if err != nil {
		return nil, err
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create claude cli stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("create claude cli stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("create claude cli stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start claude cli: %w", err)
	}

	session := &cliSession{
		cmd:             cmd,
		stdin:           stdin,
		stderrDone:      make(chan struct{}),
		events:          make(chan cliStreamMessage, 128),
		readDone:        make(chan struct{}),
		waitDone:        make(chan struct{}),
		doneCh:          make(chan struct{}),
		pendingControls: make(map[string]context.CancelFunc),
	}

	go session.readLoop(stdout)
	go session.waitLoop()
	go func() {
		defer close(session.stderrDone)
		_, _ = session.stderrBuf.ReadFrom(stderr)
	}()

	return session, nil
}

func (s *cliSession) waitLoop() {
	defer close(s.waitDone)

	var err error
	if s.cmd != nil {
		err = s.cmd.Wait()
	}

	s.mu.Lock()
	s.exited = true
	if err != nil && s.waitErr == nil {
		s.waitErr = fmt.Errorf("wait for claude cli: %w", err)
	}
	s.mu.Unlock()
}

func (a *Agent) currentCLISession() *cliSession {
	a.cliMu.Lock()
	defer a.cliMu.Unlock()
	return a.cliSession
}

func (a *Agent) resetCLISession() {
	a.cliMu.Lock()
	session := a.cliSession
	a.cliSession = nil
	a.cliMu.Unlock()

	if session != nil {
		_ = session.close()
	}
}

func (s *cliSession) readLoop(stdout io.Reader) {
	defer close(s.readDone)
	defer close(s.events)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var message cliStreamMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			s.setReadErr(fmt.Errorf("parse claude cli stream message: %w", err))
			return
		}

		select {
		case s.events <- message:
		case <-s.doneCh:
			return
		}
	}

	if err := scanner.Err(); err != nil {
		s.setReadErr(fmt.Errorf("read claude cli stream: %w", err))
	}
}

func (s *cliSession) isClosed() bool {
	if s == nil {
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed || s.exited
}

func (s *cliSession) isOperational() bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transportErrorLocked() == nil
}

func (s *cliSession) setReadErr(err error) {
	if err == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr == nil {
		s.readErr = err
	}
}

func (s *cliSession) readerError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readErr
}

func (s *cliSession) waitError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waitErr
}

func (s *cliSession) transportError() error {
	if s == nil {
		return fmt.Errorf("%w: claude cli session is nil", errCLITransportUnavailable)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transportErrorLocked()
}

func (s *cliSession) transportErrorLocked() error {
	if s.closed {
		return fmt.Errorf("%w: claude cli session is closed", errCLITransportUnavailable)
	}
	if s.readErr != nil {
		return fmt.Errorf("%w: %v", errCLITransportUnavailable, s.readErr)
	}
	if s.waitErr != nil {
		return fmt.Errorf("%w: %v", errCLITransportUnavailable, s.waitErr)
	}
	if s.exited {
		return fmt.Errorf("%w: claude cli process exited", errCLITransportUnavailable)
	}
	if s.cmd == nil || s.cmd.Process == nil {
		return fmt.Errorf("%w: claude cli process is unavailable", errCLITransportUnavailable)
	}
	if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
		return fmt.Errorf("%w: claude cli process exited", errCLITransportUnavailable)
	}
	if s.stdin == nil {
		return fmt.Errorf("%w: claude cli stdin is unavailable", errCLITransportUnavailable)
	}

	return nil
}

func (s *cliSession) nextMessage(ctx context.Context) (*cliStreamMessage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case message, ok := <-s.events:
		if !ok {
			if err := s.readerError(); err != nil {
				return nil, fmt.Errorf("%w: %v", errCLITransportUnavailable, err)
			}
			if err := s.waitError(); err != nil {
				return nil, fmt.Errorf("%w: %v", errCLITransportUnavailable, err)
			}
			return nil, fmt.Errorf("%w: stream closed before result", errCLITransportUnavailable)
		}
		return &message, nil
	}
}

func (s *cliSession) writeTurn(payload string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.transportErrorLocked(); err != nil {
		return err
	}

	if _, err := io.WriteString(s.stdin, payload); err != nil {
		return fmt.Errorf("%w: write claude cli input: %w", errCLITransportUnavailable, err)
	}
	return nil
}

func (s *cliSession) writeJSON(value interface{}) error {
	bytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal claude cli input: %w", err)
	}

	return s.writeTurn(string(bytes) + "\n")
}

func (s *cliSession) updateEnvironment(values map[string]string) error {
	filtered := filterCLIEnvUpdates(values)
	if len(filtered) == 0 {
		return nil
	}

	return s.writeJSON(cliUpdateEnvironmentMessage{
		Type:      "update_environment_variables",
		Variables: filtered,
	})
}

func (s *cliSession) sendControlResponseSuccess(requestID string, response interface{}) error {
	if strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("control response requires request id")
	}

	return s.writeJSON(cliControlResponseEnvelope{
		Type: "control_response",
		Response: cliControlResponsePayload{
			Subtype:   "success",
			RequestID: requestID,
			Response:  response,
		},
	})
}

func (s *cliSession) sendControlResponseError(requestID, errorText string) error {
	if strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("control response requires request id")
	}

	return s.writeJSON(cliControlResponseEnvelope{
		Type: "control_response",
		Response: cliControlResponsePayload{
			Subtype:   "error",
			RequestID: requestID,
			Error:     strings.TrimSpace(errorText),
		},
	})
}

func (s *cliSession) sendControlCancel(requestID string) error {
	if strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("control cancel requires request id")
	}

	return s.writeJSON(cliControlCancelRequest{
		Type:      "control_cancel_request",
		RequestID: requestID,
	})
}

func (s *cliSession) beginControlRequest(requestID string, cancel context.CancelFunc) bool {
	if strings.TrimSpace(requestID) == "" || cancel == nil {
		return false
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if _, exists := s.pendingControls[requestID]; exists {
		return false
	}
	s.pendingControls[requestID] = cancel
	return true
}

func (s *cliSession) finishControlRequest(requestID string) {
	s.controlMu.Lock()
	cancel, ok := s.pendingControls[requestID]
	if ok {
		delete(s.pendingControls, requestID)
	}
	s.controlMu.Unlock()

	if ok {
		cancel()
	}
}

func (s *cliSession) cancelAllControlRequests() {
	s.controlMu.Lock()
	pending := make(map[string]context.CancelFunc, len(s.pendingControls))
	for requestID, cancel := range s.pendingControls {
		pending[requestID] = cancel
		delete(s.pendingControls, requestID)
	}
	s.controlMu.Unlock()

	for requestID, cancel := range pending {
		cancel()
		_ = s.sendControlCancel(requestID)
	}
}

func (s *cliSession) close() error {
	s.controlMu.Lock()
	pending := make([]context.CancelFunc, 0, len(s.pendingControls))
	for requestID, cancel := range s.pendingControls {
		pending = append(pending, cancel)
		delete(s.pendingControls, requestID)
	}
	s.controlMu.Unlock()

	for _, cancel := range pending {
		cancel()
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.doneCh)
	stdin := s.stdin
	cmd := s.cmd
	readDone := s.readDone
	waitDone := s.waitDone
	stderrDone := s.stderrDone
	s.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}

	if readDone != nil {
		<-readDone
	}
	if waitDone != nil {
		<-waitDone
	}
	if stderrDone != nil {
		<-stderrDone
	}

	if cmd == nil {
		return nil
	}
	return s.waitError()
}

func (s *cliSession) stderrText() string {
	if s == nil {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.stderrBuf.String())
}
