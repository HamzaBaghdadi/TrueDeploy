// Package sshx wraps golang.org/x/crypto/ssh with the connection
// settings TrueDeploy needs: password or private-key auth, a dial
// timeout, and trust-on-first-use host key checking.
package sshx

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

type AuthMethod string

const (
	AuthPassword AuthMethod = "password"
	AuthKey      AuthMethod = "key"
)

// Target describes how to reach and authenticate to a server.
type Target struct {
	Host       string     `json:"host"`
	Port       int        `json:"port"`
	User       string     `json:"user"`
	Auth       AuthMethod `json:"auth"`
	Password   string     `json:"password,omitempty"`
	PrivateKey string     `json:"privateKey,omitempty"` // PEM contents
	KeyPath    string     `json:"keyPath,omitempty"`    // used when PrivateKey is empty
	Passphrase string     `json:"passphrase,omitempty"`
	// HostKeyFingerprint is the SHA256 fingerprint the server must present.
	// Empty means "first connection": any key is accepted and reported back.
	HostKeyFingerprint string `json:"hostKeyFingerprint,omitempty"`
}

var ErrHostKeyMismatch = errors.New("host key mismatch: the server's identity changed (possible man-in-the-middle)")

// Client is an open SSH connection plus the fingerprint the server presented.
type Client struct {
	*ssh.Client
	Fingerprint string
}

func Dial(t Target, timeout time.Duration) (*Client, error) {
	if t.Host == "" || t.User == "" {
		return nil, errors.New("host and user are required")
	}
	if t.Port == 0 {
		t.Port = 22
	}

	auth, err := authMethods(t)
	if err != nil {
		return nil, err
	}

	var seen string
	cfg := &ssh.ClientConfig{
		User:    t.User,
		Auth:    auth,
		Timeout: timeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = ssh.FingerprintSHA256(key)
			if t.HostKeyFingerprint != "" && t.HostKeyFingerprint != seen {
				return ErrHostKeyMismatch
			}
			return nil
		},
	}

	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	c, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, err
	}
	return &Client{Client: c, Fingerprint: seen}, nil
}

func authMethods(t Target) ([]ssh.AuthMethod, error) {
	switch t.Auth {
	case AuthPassword:
		if t.Password == "" {
			return nil, errors.New("password is required")
		}
		// Some servers only offer keyboard-interactive for passwords.
		kbd := ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			ans := make([]string, len(qs))
			for i := range ans {
				ans[i] = t.Password
			}
			return ans, nil
		})
		return []ssh.AuthMethod{ssh.Password(t.Password), kbd}, nil

	case AuthKey:
		pem := []byte(t.PrivateKey)
		if len(pem) == 0 {
			if t.KeyPath == "" {
				return nil, errors.New("private key or key path is required")
			}
			b, err := os.ReadFile(t.KeyPath)
			if err != nil {
				return nil, fmt.Errorf("read key file: %w", err)
			}
			pem = b
		}
		var signer ssh.Signer
		var err error
		if t.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(t.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(pem)
		}
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, errors.New("this key is encrypted: enter its passphrase")
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	return nil, fmt.Errorf("unknown auth method %q", t.Auth)
}

// Run executes a command and returns combined stdout/stderr.
func (c *Client) Run(cmd string) (string, error) {
	s, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	// CombinedOutput serialises writes; sharing one bytes.Buffer between
	// Stdout and Stderr races (both copiers use Buffer.ReadFrom) and drops output.
	out, err := s.CombinedOutput(cmd)
	return string(out), err
}
