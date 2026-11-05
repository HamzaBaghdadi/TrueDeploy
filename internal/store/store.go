// Package store persists saved servers. Non-secret fields live in
// servers.json; passwords, keys and passphrases are AES-GCM encrypted in
// secrets.json with a random master key held in the OS keychain
// (Windows Credential Manager, macOS Keychain, Linux Secret Service).
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	appName        = "TrueDeploy"
	keyringAccount = "vault-master-key"
)

type Server struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Host               string    `json:"host"`
	Port               int       `json:"port"`
	User               string    `json:"user"`
	Auth               string    `json:"auth"`
	KeyPath            string    `json:"keyPath,omitempty"`
	HostKeyFingerprint string    `json:"hostKeyFingerprint,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
}

type Secrets struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type Store struct {
	mu  sync.Mutex
	dir string
}

func Open() (*Store, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, appName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) List() ([]Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readServers()
	if err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) Get(id string) (Server, Secrets, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readServers()
	if err != nil {
		return Server{}, Secrets{}, err
	}
	srv, ok := m[id]
	if !ok {
		return Server{}, Secrets{}, fmt.Errorf("server %s not found", id)
	}
	sec, err := s.readSecrets()
	if err != nil {
		return Server{}, Secrets{}, err
	}
	return srv, sec[id], nil
}

func (s *Store) Put(srv Server, sec Secrets) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readServers()
	if err != nil {
		return err
	}
	all, err := s.readSecrets()
	if err != nil {
		return err
	}
	m[srv.ID] = srv
	all[srv.ID] = sec
	if err := s.writeSecrets(all); err != nil {
		return err
	}
	return writeJSON(s.path("servers.json"), m)
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readServers()
	if err != nil {
		return err
	}
	all, err := s.readSecrets()
	if err != nil {
		return err
	}
	delete(m, id)
	delete(all, id)
	if err := s.writeSecrets(all); err != nil {
		return err
	}
	return writeJSON(s.path("servers.json"), m)
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

func (s *Store) readServers() (map[string]Server, error) {
	m := map[string]Server{}
	err := readJSON(s.path("servers.json"), &m)
	return m, err
}

func (s *Store) readSecrets() (map[string]Secrets, error) {
	m := map[string]Secrets{}
	return m, s.readEncrypted("secrets.json", &m)
}

func (s *Store) writeSecrets(m map[string]Secrets) error {
	return s.writeEncrypted("secrets.json", m)
}

// readEncrypted decrypts a JSON file written by writeEncrypted into v.
// A missing file leaves v unchanged.
func (s *Store) readEncrypted(name string, v any) error {
	raw, err := os.ReadFile(s.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var blob string
	if err := json.Unmarshal(raw, &blob); err != nil {
		return err
	}
	plain, err := s.decrypt(blob)
	if err != nil {
		return fmt.Errorf("decrypt %s: %w", name, err)
	}
	return json.Unmarshal(plain, v)
}

func (s *Store) writeEncrypted(name string, v any) error {
	plain, err := json.Marshal(v)
	if err != nil {
		return err
	}
	blob, err := s.encrypt(plain)
	if err != nil {
		return err
	}
	return writeJSON(s.path(name), blob)
}
func masterKey() ([]byte, error) {
	enc, err := keyring.Get(appName, keyringAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, err
		}
		if err := keyring.Set(appName, keyringAccount, base64.StdEncoding.EncodeToString(k)); err != nil {
			return nil, fmt.Errorf("save master key to OS keychain: %w", err)
		}
		return k, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read master key from OS keychain: %w", err)
	}
	return base64.StdEncoding.DecodeString(enc)
}

func gcm() (cipher.AEAD, error) {
	k, err := masterKey()
	if err != nil {
		return nil, err
	}
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func (s *Store) encrypt(plain []byte) (string, error) {
	g, err := gcm()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, plain, nil)), nil
}

func (s *Store) decrypt(blob string) ([]byte, error) {
	g, err := gcm()
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, err
	}
	if len(raw) < g.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	return g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON writes atomically so a crash never leaves a half-written file.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
