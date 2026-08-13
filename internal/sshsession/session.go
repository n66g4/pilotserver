package sshsession

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

var ErrAuthFailed = errors.New("SSH authentication failed")

type Session struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

func Connect(ctx context.Context, addr string, signer ssh.Signer, cols, rows int) (*Session, error) {
	config := &ssh.ClientConfig{
		User:            "comma",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	handshakeDeadline := time.Now().Add(10 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(handshakeDeadline) {
		handshakeDeadline = deadline
	}
	if err := conn.SetDeadline(handshakeDeadline); err != nil {
		conn.Close()
		return nil, err
	}
	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	clientConn, channels, requests, err := ssh.NewClientConn(conn, addr, config)
	stopCancel()
	if err != nil {
		conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, fmt.Errorf("%w: %v", ErrAuthFailed, err)
		}
		return nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		clientConn.Close()
		return nil, err
	}
	client := ssh.NewClient(clientConn, channels, requests)

	sshSession, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	if err := sshSession.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		sshSession.Close()
		client.Close()
		return nil, err
	}
	stdin, err := sshSession.StdinPipe()
	if err != nil {
		sshSession.Close()
		client.Close()
		return nil, err
	}
	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		sshSession.Close()
		client.Close()
		return nil, err
	}
	if err := sshSession.Shell(); err != nil {
		sshSession.Close()
		client.Close()
		return nil, err
	}
	return &Session{
		client:  client,
		session: sshSession,
		stdin:   stdin,
		stdout:  stdout,
	}, nil
}

func (s *Session) Stdin() io.WriteCloser {
	return s.stdin
}

func (s *Session) Stdout() io.Reader {
	return s.stdout
}

func (s *Session) Resize(cols, rows int) error {
	return s.session.WindowChange(rows, cols)
}

func (s *Session) Wait() error {
	return s.session.Wait()
}

func (s *Session) Close() error {
	return errors.Join(s.session.Close(), s.client.Close())
}
