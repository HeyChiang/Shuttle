package sshcore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
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

// BatchExec runs command on each profile over a short-lived connection, so the
// hosts do not need an open terminal session. Attachments are uploaded to
// targetDir before the script starts. Progress and results are pushed through
// EventBatchExec; runID lets the caller ignore stale runs.
func (m *Manager) BatchExec(cfgs []HostConfig, command string, files []string, targetDir, runID string) {
	command = strings.TrimRight(command, "\r\n")
	if strings.TrimSpace(command) == "" || len(cfgs) == 0 {
		return
	}
	for _, cfg := range cfgs {
		cfg = applyDefaults(cfg)
		go func() {
			start := BatchExecEvent{
				RunID:   runID,
				HostID:  cfg.ID,
				Name:    cfg.Name,
				Command: command,
				Status:  "running",
			}
			if len(files) > 0 {
				start.Status = "uploading"
			}
			if start.HostID == "" {
				start.HostID = cfg.Host
			}
			m.emit(EventBatchExec, start)

			// 附件传完再执行脚本，让前端把状态从“上传中”切到“执行中”。
			onUploaded := func() {
				start.Status = "running"
				m.emit(EventBatchExec, start)
			}
			output, exitCode, err := runOnProfile(cfg, command, files, targetDir, onUploaded)
			result := start
			result.Output = output
			result.ExitCode = exitCode
			result.Status = "done"
			if err != nil {
				result.Error = err.Error()
				// 退出码 >= 0 说明连接和命令都正常，只是脚本本身返回非零。
				if exitCode < 0 {
					result.Status = "error"
				}
			}
			m.emit(EventBatchExec, result)
		}()
	}
}

// runOnProfile connects with a profile's stored credentials, uploads attachments
// when configured, runs the command once and returns decoded combined output
// plus the remote exit status.
func runOnProfile(cfg HostConfig, command string, files []string, targetDir string, onUploaded func()) (string, int, error) {
	client, err := dialClient(cfg)
	if err != nil {
		return "", -1, err
	}
	defer func() { _ = client.Close() }()

	if len(files) > 0 {
		if err := uploadFilesToProfile(client, files, targetDir); err != nil {
			return "", -1, fmt.Errorf("failed to upload attachments: %w", err)
		}
		if onUploaded != nil {
			onUploaded()
		}
	}

	sshSession, err := client.NewSession()
	if err != nil {
		return "", -1, err
	}
	defer func() { _ = sshSession.Close() }()

	raw, err := sshSession.CombinedOutput(command)
	output, _, _ := transform.String(decoderFor(cfg.Encoding), string(raw))
	if err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			return output, exitErr.ExitStatus(), err
		}
		return output, -1, err
	}
	return output, 0, nil
}

// uploadFilesToProfile copies local attachments into targetDir on the remote
// host, creating the directory (and its parents) when missing.
func uploadFilesToProfile(client *ssh.Client, files []string, targetDir string) error {
	c, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("failed to open SFTP: %w", err)
	}
	defer func() { _ = c.Close() }()

	dir := strings.TrimSpace(targetDir)
	absDir := path.Clean(dir)
	if !strings.HasPrefix(dir, "/") {
		home, err := c.Getwd()
		if err != nil || home == "" {
			home = "/"
		}
		absDir = path.Join(home, dir)
	}
	if err := c.MkdirAll(absDir); err != nil {
		return fmt.Errorf("failed to create remote directory %s: %w", absDir, err)
	}

	for _, localPath := range files {
		stat, err := os.Stat(localPath)
		if err != nil {
			return fmt.Errorf("failed to read local file %s: %w", filepath.Base(localPath), err)
		}
		if stat.IsDir() {
			return fmt.Errorf("%s is a directory; directory upload is not supported", filepath.Base(localPath))
		}
		if err := sendLocalFile(c, localPath, absDir, nil); err != nil {
			return fmt.Errorf("failed to upload %s: %w", filepath.Base(localPath), err)
		}
	}
	return nil
}

func encodingFor(name string) encoding.Encoding {
	if enc, ok := encodings[strings.ToUpper(name)]; ok {
		return enc
	}
	return encodings[EncodingUTF8]
}