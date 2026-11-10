package sshcore

import (
	"fmt"
	"net"
	"time"

	"github.com/xanzy/ssh-agent"
	"golang.org/x/crypto/ssh"
)

// authMethodsFor builds the SSH authentication methods for a profile.
func authMethodsFor(cfg HostConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	switch cfg.AuthMethod {
	case AuthKey:
		signer, err := loadSigner(cfg.PrivateKeyPath, cfg.KeyPassphrase)
		if err != nil {
			return nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	case AuthAgent:
		agentClient, _, err := sshagent.New()
		if err != nil {
			return nil, fmt.Errorf("failed to connect to ssh-agent: %w", err)
		}
		methods = append(methods, ssh.PublicKeysCallback(agentClient.Signers))
	default:
		methods = append(methods, ssh.Password(cfg.Password))
	}
	// PAM based servers use keyboard-interactive for the same password.
	if cfg.Password != "" {
		password := cfg.Password
		methods = append(methods, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = password
			}
			return answers, nil
		}))
	}
	return methods, nil
}

// dialClient opens an SSH client connection for a profile without requesting a
// PTY, so it can be reused by terminals and standalone SFTP transfers alike.
func dialClient(cfg HostConfig) (*ssh.Client, error) {
	auths, err := authMethodsFor(cfg)
	if err != nil {
		return nil, err
	}
	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(cfg.TimeoutSec) * time.Second,
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	conn, err := net.DialTimeout("tcp", addr, clientCfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, clientCfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH handshake failed: %w", err)
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}
