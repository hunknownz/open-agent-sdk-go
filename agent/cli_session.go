package agent

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type cliSession struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	scanner    *bufio.Scanner
	stderrBuf  bytes.Buffer
	stderrDone chan struct{}
	closed     bool
}

func (a *Agent) ensureCLISession(ctx context.Context) error {
	_ = ctx
	a.cliMu.Lock()
	defer a.cliMu.Unlock()

	if a.cliSession != nil && !a.cliSession.isClosed() {
		return nil
	}

	session, err := a.startCLISession(context.Background())
	if err != nil {
		return err
	}
	a.cliSession = session
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
		cmd:        cmd,
		stdin:      stdin,
		scanner:    bufio.NewScanner(stdout),
		stderrDone: make(chan struct{}),
	}
	session.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	go func() {
		defer close(session.stderrDone)
		_, _ = session.stderrBuf.ReadFrom(stderr)
	}()

	return session, nil
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

func (s *cliSession) isClosed() bool {
	if s == nil {
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *cliSession) writeTurn(payload string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("claude cli session is closed")
	}

	if _, err := io.WriteString(s.stdin, payload); err != nil {
		return fmt.Errorf("write claude cli input: %w", err)
	}

	return nil
}

func (s *cliSession) close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	stdin := s.stdin
	cmd := s.cmd
	stderrDone := s.stderrDone
	s.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}

	var waitErr error
	if cmd != nil {
		waitErr = cmd.Wait()
	}

	if stderrDone != nil {
		<-stderrDone
	}

	return waitErr
}

func (s *cliSession) stderrText() string {
	if s == nil {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.stderrBuf.String())
}
