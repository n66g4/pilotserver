package sshsession_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"pilotserver/internal/sshsession"
)

func TestConnectStartsPTYSession(t *testing.T) {
	signer := newSigner(t)
	addr := startSSHServer(t, signer.PublicKey())

	session, err := sshsession.Connect(context.Background(), addr, signer, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	greeting := make([]byte, len("ready\n"))
	if _, err := io.ReadFull(session.Stdout(), greeting); err != nil {
		t.Fatal(err)
	}
	if string(greeting) != "ready\n" {
		t.Fatalf("greeting = %q, want %q", greeting, "ready\n")
	}
}

func TestConnectMapsRejectedKeyToAuthFailed(t *testing.T) {
	allowed := newSigner(t)
	addr := startSSHServer(t, allowed.PublicKey())

	session, err := sshsession.Connect(context.Background(), addr, newSigner(t), 80, 24)
	if session != nil {
		session.Close()
	}
	if !errors.Is(err, sshsession.ErrAuthFailed) {
		t.Fatalf("Connect error = %v, want ErrAuthFailed", err)
	}
}

func TestConnectStopsSilentHandshakeWhenContextExpires(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	signer := newSigner(t)
	result := make(chan error, 1)
	started := time.Now()
	go func() {
		session, err := sshsession.Connect(ctx, listener.Addr().String(), signer, 80, 24)
		if session != nil {
			_ = session.Close()
		}
		result <- err
	}()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Connect returned nil error for silent SSH server")
		}
		if elapsed := time.Since(started); elapsed >= 2*time.Second {
			t.Fatalf("Connect returned after %s, want under 2s", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("Connect did not stop after context deadline")
	}
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func startSSHServer(t *testing.T, allowed ssh.PublicKey) string {
	t.Helper()
	hostSigner := newSigner(t)
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if !bytes.Equal(key.Marshal(), allowed.Marshal()) {
				return nil, errors.New("unknown key")
			}
			return nil, nil
		},
	}
	config.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveSSHConnection(conn, config)
		}
	}()
	return listener.Addr().String()
}

func serveSSHConnection(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()
	_, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for channelRequest := range channels {
		if channelRequest.ChannelType() != "session" {
			_ = channelRequest.Reject(ssh.UnknownChannelType, "session required")
			continue
		}
		channel, requests, err := channelRequest.Accept()
		if err != nil {
			return
		}
		go func() {
			defer channel.Close()
			for request := range requests {
				switch request.Type {
				case "pty-req":
					_ = request.Reply(true, nil)
				case "shell":
					_ = request.Reply(true, nil)
					_, _ = channel.Write([]byte("ready\n"))
					_, _ = io.Copy(channel, channel)
				default:
					_ = request.Reply(false, nil)
				}
			}
		}()
	}
}
