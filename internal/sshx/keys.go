package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// NewKeyPair generates an ed25519 login key: the private key in OpenSSH PEM
// form and the public key as an authorized_keys line.
func NewKeyPair(comment string) (privatePEM, authorized string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return "", "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " " + comment
	return string(pem.EncodeToMemory(block)), line, nil
}

// PublicKeyOf returns the "type base64" part of a target's key (for removing
// it from authorized_keys), or "" when it uses a password.
func PublicKeyOf(t Target) (string, error) {
	if t.Auth != AuthKey {
		return "", nil
	}
	data := []byte(t.PrivateKey)
	if len(data) == 0 {
		var err error
		if data, err = os.ReadFile(t.KeyPath); err != nil {
			return "", err
		}
	}
	var signer ssh.Signer
	var err error
	if t.Passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(t.Passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(data)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}
