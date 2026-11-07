package sshx

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startServer runs an in-process SSH server that accepts password "secret"
// or the given client public key, and answers every exec with "ran: <cmd>".
func startServer(t *testing.T, clientKey ssh.PublicKey) (host string, port int, fingerprint string) {
	t.Helper()
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if string(pw) == "secret" {
				return nil, nil
			}
			return nil, errors.New("bad password")
		},
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if clientKey != nil && string(k.Marshal()) == string(clientKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(nc, cfg)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, ssh.FingerprintSHA256(hostSigner.PublicKey())
}

func serve(nc net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		ch, creqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			for req := range creqs {
				switch req.Type {
				case "pty-req":
					var p struct {
						Term          string
						Cols, Rows    uint32
						Width, Height uint32
						Modes         string
					}
					ssh.Unmarshal(req.Payload, &p)
					lastPTY.Store(fmt.Sprintf("%s %dx%d", p.Term, p.Cols, p.Rows))
					req.Reply(true, nil)
					continue
				case "window-change":
					var p struct{ Cols, Rows, Width, Height uint32 }
					ssh.Unmarshal(req.Payload, &p)
					lastWindow.Store(fmt.Sprintf("%dx%d", p.Cols, p.Rows))
					continue
				case "shell":
					req.Reply(true, nil)
					// A tiny "shell": echo lines back until "exit".
					go func() {
						ch.Write([]byte("$ "))
						buf := make([]byte, 1024)
						var line []byte
						for {
							n, err := ch.Read(buf)
							if err != nil {
								return
							}
							ch.Write(buf[:n])
							line = append(line, buf[:n]...)
							if i := bytes.IndexByte(line, '\n'); i >= 0 {
								if strings.TrimSpace(string(line[:i])) == "exit" {
									ch.CloseWrite()
									ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
									ch.Close()
									return
								}
								line = line[i+1:]
								ch.Write([]byte("$ "))
							}
						}
					}()
					continue
				}
				if req.Type != "exec" {
					req.Reply(false, nil)
					continue
				}
				var p struct{ Cmd string }
				ssh.Unmarshal(req.Payload, &p)
				req.Reply(true, nil)
				ch.Write([]byte("ran: " + p.Cmd))
				ch.CloseWrite()
				ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			}
		}()
	}
}

var lastPTY, lastWindow atomic.Value

func TestInteractiveShell(t *testing.T) {
	host, port, _ := startServer(t, nil)
	c, err := Dial(Target{Host: host, Port: port, User: "root", Auth: AuthPassword, Password: "secret"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var mu sync.Mutex
	var out bytes.Buffer
	sh, err := c.OpenShell(120, 40, func(b []byte) {
		mu.Lock()
		out.Write(b)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := lastPTY.Load().(string); got != "xterm-256color 120x40" {
		t.Fatalf("pty request = %q", got)
	}

	if err := sh.Write([]byte("echo héllo\n")); err != nil {
		t.Fatal(err)
	}
	if err := sh.Resize(200, 50); err != nil {
		t.Fatal(err)
	}
	waitFor := func(cond func() bool) bool {
		for i := 0; i < 100; i++ {
			if cond() {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	if !waitFor(func() bool { mu.Lock(); defer mu.Unlock(); return strings.Contains(out.String(), "echo héllo") }) {
		t.Fatalf("typed text not echoed; output: %q", out.String())
	}
	if !waitFor(func() bool { s, _ := lastWindow.Load().(string); return s == "200x50" }) {
		t.Fatalf("resize not received: %v", lastWindow.Load())
	}

	sh.Write([]byte("exit\n"))
	select {
	case <-sh.Done():
		if sh.Err() != nil {
			t.Fatalf("clean exit reported an error: %v", sh.Err())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shell didn't finish after exit")
	}
	sh.Close() // idempotent after exit
}

func TestPasswordAuthAndRun(t *testing.T) {
	host, port, fp := startServer(t, nil)
	c, err := Dial(Target{Host: host, Port: port, User: "root", Auth: AuthPassword, Password: "secret"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Fingerprint != fp {
		t.Fatalf("fingerprint = %q, want %q", c.Fingerprint, fp)
	}
	out, err := c.Run("uname -r")
	if err != nil || out != "ran: uname -r" {
		t.Fatalf("Run = %q, %v", out, err)
	}
}

func TestStreamDeliversLines(t *testing.T) {
	host, port, _ := startServer(t, nil)
	c, err := Dial(Target{Host: host, Port: port, User: "root", Auth: AuthPassword, Password: "secret"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 20; i++ {
		var lines []string
		err := c.Stream(context.Background(), "apt-get update", "", func(stream, line string) {
			lines = append(lines, stream+":"+line)
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) != 1 || lines[0] != "stdout:ran: apt-get update" {
			t.Fatalf("lines = %q", lines)
		}
	}
}

func TestWrongPassword(t *testing.T) {
	host, port, _ := startServer(t, nil)
	_, err := Dial(Target{Host: host, Port: port, User: "root", Auth: AuthPassword, Password: "nope"}, 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatalf("err = %v, want auth failure", err)
	}
}

func TestPinnedHostKeyMismatch(t *testing.T) {
	host, port, _ := startServer(t, nil)
	_, err := Dial(Target{
		Host: host, Port: port, User: "root", Auth: AuthPassword, Password: "secret",
		HostKeyFingerprint: "SHA256:not-the-real-one",
	}, 5*time.Second)
	if !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("err = %v, want ErrHostKeyMismatch", err)
	}
}

func TestPinnedHostKeyMatch(t *testing.T) {
	host, port, fp := startServer(t, nil)
	c, err := Dial(Target{
		Host: host, Port: port, User: "root", Auth: AuthPassword, Password: "secret",
		HostKeyFingerprint: fp,
	}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestPrivateKeyAuth(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	host, port, _ := startServer(t, sshPub)

	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Dial(Target{
		Host: host, Port: port, User: "deploy", Auth: AuthKey,
		PrivateKey: string(pem.EncodeToMemory(block)),
	}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestEncryptedKeyNeedsPassphrase(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Dial(Target{
		Host: "127.0.0.1", Port: 1, User: "x", Auth: AuthKey,
		PrivateKey: string(pem.EncodeToMemory(block)),
	}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("err = %v, want passphrase prompt", err)
	}
}

func TestConnectionRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	_, err := Dial(Target{Host: "127.0.0.1", Port: port, User: "x", Auth: AuthPassword, Password: "p"}, 2*time.Second)
	if err == nil {
		t.Fatal("expected error dialing closed port " + strconv.Itoa(port))
	}
}

func TestOpenCommandRunsOnAPTY(t *testing.T) {
	host, port, fp := startServer(t, nil)
	c, err := Dial(Target{Host: host, Port: port, User: "u", Auth: AuthPassword, Password: "secret", HostKeyFingerprint: fp}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var mu sync.Mutex
	var got bytes.Buffer
	sh, err := c.OpenCommand(100, 30, "docker exec -it web sh", func(b []byte) {
		mu.Lock()
		got.Write(b)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-sh.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("command didn't finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(got.String(), "ran: docker exec -it web sh") {
		t.Errorf("output %q", got.String())
	}
	if v, _ := lastPTY.Load().(string); !strings.Contains(v, "100x30") {
		t.Errorf("pty %q", v)
	}
}

func TestNewKeyPairLogsIn(t *testing.T) {
	priv, _, err := NewKeyPair("truedeploy-login-test")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey([]byte(priv))
	if err != nil {
		t.Fatal(err)
	}
	host, port, fp := startServer(t, signer.PublicKey())
	c, err := Dial(Target{Host: host, Port: port, User: "u", Auth: AuthKey, PrivateKey: priv, HostKeyFingerprint: fp}, 5*time.Second)
	if err != nil {
		t.Fatalf("a generated key must log in: %v", err)
	}
	c.Close()
}
