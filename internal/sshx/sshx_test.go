package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"strconv"
	"strings"
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
