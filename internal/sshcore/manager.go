package sshcore

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

// Emitter forwards an event to the host application (Wails event bus).
type Emitter func(event string, payload any)

// Manager owns every live session and routes input to them.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*session
	emit     Emitter
}

// NewManager creates an empty session manager.
func NewManager(emit Emitter) *Manager {
	if emit == nil {
		emit = func(string, any) {}
	}
	return &Manager{sessions: make(map[string]*session), emit: emit}
}

func (m *Manager) get(id string) *session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// Connect opens a session for cfg. cfg.SessionID is used as the routing key and
// must be supplied by the caller; it falls back to a generated identifier.
func (m *Manager) Connect(cfg HostConfig) (SessionInfo, error) {
	cfg = applyDefaults(cfg)
	if strings.TrimSpace(cfg.Host) == "" {
		return SessionInfo{}, fmt.Errorf("host address is required")
	}
	if cfg.SessionID == "" {
		cfg.SessionID = fmt.Sprintf("%s@%s:%d-%d", cfg.User, cfg.Host, cfg.Port, time.Now().UnixNano())
	}

	m.mu.Lock()
	if _, exists := m.sessions[cfg.SessionID]; exists {
		m.mu.Unlock()
		return SessionInfo{}, fmt.Errorf("session %s already exists", cfg.SessionID)
	}
	s := newSession(cfg, m.emit)
	m.sessions[cfg.SessionID] = s
	m.mu.Unlock()

	m.emit(EventStatus, StatusEvent{SessionID: cfg.SessionID, Status: StatusConnecting, Message: "Connecting to " + cfg.Host})
	if err := s.start(); err != nil {
		s.close()
		m.mu.Lock()
		delete(m.sessions, cfg.SessionID)
		m.mu.Unlock()
		m.emit(EventStatus, StatusEvent{SessionID: cfg.SessionID, Status: StatusError, Message: err.Error()})
		return SessionInfo{}, err
	}
	m.emit(EventStatus, StatusEvent{SessionID: cfg.SessionID, Status: StatusConnected})
	return s.info(), nil
}

// Disconnect closes one session and forgets it.
func (m *Manager) Disconnect(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		s.close()
	}
}

// DisconnectAll closes every session.
func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	all := m.sessions
	m.sessions = make(map[string]*session)
	m.mu.Unlock()
	for _, s := range all {
		s.close()
	}
}

// Sessions returns a stable snapshot ordered by connection time.
func (m *Manager) Sessions() []SessionInfo {
	m.mu.RLock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.info())
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Count returns the number of tracked sessions.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Write sends data to the given sessions. An empty list broadcasts to every
// connected session, which is what the "sync input" mode uses.
func (m *Manager) Write(ids []string, data string) error {
	targets := m.resolve(ids)
	if len(targets) == 0 {
		return fmt.Errorf("no available sessions")
	}
	var firstErr error
	for _, s := range targets {
		if err := s.write([]byte(data)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Resize applies a terminal size to the given sessions.
func (m *Manager) Resize(ids []string, cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	for _, s := range m.resolve(ids) {
		_ = s.resize(cols, rows)
	}
}

func (m *Manager) resolve(ids []string) []*session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(ids) == 0 {
		out := make([]*session, 0, len(m.sessions))
		for _, s := range m.sessions {
			if !s.isClosed() {
				out = append(out, s)
			}
		}
		return out
	}
	out := make([]*session, 0, len(ids))
	for _, id := range ids {
		if s, ok := m.sessions[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

// Exec runs a one-shot command on each session in its own SSH channel and
// reports the output through the EventExec event.
func (m *Manager) Exec(ids []string, command string) {
	command = strings.TrimRight(command, "\r\n")
	if strings.TrimSpace(command) == "" {
		return
	}
	targets := m.resolve(ids)
	for _, s := range targets {
		go m.execOne(s, command)
	}
}

func (m *Manager) execOne(s *session, command string) {
	result := ExecEvent{SessionID: s.cfg.SessionID, Name: s.cfg.Name, Command: command}
	defer func() { m.emit(EventExec, result) }()

	if s.client == nil || s.isClosed() {
		result.Error = "session is closed"
		result.ExitCode = -1
		return
	}
	sshSession, err := s.client.NewSession()
	if err != nil {
		result.Error = err.Error()
		result.ExitCode = -1
		return
	}
	defer func() { _ = sshSession.Close() }()

	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		result.Error = err.Error()
		result.ExitCode = -1
		return
	}
	stderr, err := sshSession.StderrPipe()
	if err != nil {
		result.Error = err.Error()
		result.ExitCode = -1
		return
	}
	if err := sshSession.Start(command); err != nil {
		result.Error = err.Error()
		result.ExitCode = -1
		return
	}

	decoder := s.decoderTransform()
	outBytes, _ := io.ReadAll(transform.NewReader(stdout, decoder))
	errBytes, _ := io.ReadAll(transform.NewReader(stderr, decoderFor(s.cfg.Encoding)))
	result.Output = string(outBytes)
	if len(errBytes) > 0 {
		result.Output += string(errBytes)
	}

	err = sshSession.Wait()
	switch e := err.(type) {
	case nil:
		result.ExitCode = 0
	case *ssh.ExitError:
		result.ExitCode = e.ExitStatus()
		result.Error = fmt.Sprintf("exit code %d", e.ExitStatus())
	default:
		result.ExitCode = -1
		result.Error = err.Error()
	}
}

func decoderFor(name string) transform.Transformer {
	if isUTF8(name) {
		return transform.Nop
	}
	return encodingFor(name).NewDecoder()
}

func encodingFor(name string) encoding.Encoding {
	if enc, ok := encodings[strings.ToUpper(name)]; ok {
		return enc
	}
	return encodings[EncodingUTF8]
}