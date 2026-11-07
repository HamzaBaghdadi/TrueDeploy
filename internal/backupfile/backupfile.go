// Package backupfile seals TrueDeploy's exported data with a passphrase:
// Argon2id derives a 256-bit key, AES-256-GCM encrypts and authenticates
// the data. Everything needed to open the file except the passphrase is in
// its JSON envelope, so it can be imported on any computer.
package backupfile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	Format  = "truedeploy-backup"
	Version = 1
	// MinPassphrase is the shortest passphrase accepted for new files.
	MinPassphrase = 10
)

type envelope struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	KDF     string `json:"kdf"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memoryKiB"`
	Threads uint8  `json:"threads"`
	Salt    []byte `json:"salt"`
	Nonce   []byte `json:"nonce"`
	Data    []byte `json:"data"`
}

// Argon2id parameters (OWASP's recommended minimum is 19 MiB / 2 passes;
// these are stronger and still take well under a second).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
)

// ErrWrongPassphrase covers a wrong passphrase and a damaged file alike:
// GCM can't tell them apart, and neither should an attacker.
var ErrWrongPassphrase = errors.New("wrong passphrase, or the file is damaged")

func key(pass string, salt []byte, t, m uint32, p uint8) []byte {
	return argon2.IDKey([]byte(pass), salt, t, m, p, 32)
}

// CheckPassphrase validates a passphrase for a new file.
func CheckPassphrase(pass string) error {
	if utf8.RuneCountInString(pass) < MinPassphrase {
		return fmt.Errorf("use a passphrase of at least %d characters", MinPassphrase)
	}
	return nil
}

// Seal encrypts plain with the passphrase.
func Seal(plain []byte, pass string) ([]byte, error) {
	if err := CheckPassphrase(pass); err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	env := envelope{Format: Format, Version: Version, KDF: "argon2id", Time: argonTime, Memory: argonMemory, Threads: argonThreads, Salt: salt, Nonce: nonce}
	gcm, err := newGCM(key(pass, salt, env.Time, env.Memory, env.Threads))
	if err != nil {
		return nil, err
	}
	// The header is authenticated too, so its parameters can't be tampered with.
	env.Data = gcm.Seal(nil, nonce, plain, header(env))
	return json.MarshalIndent(env, "", "  ")
}

// Open decrypts a file made by Seal.
func Open(file []byte, pass string) ([]byte, error) {
	var env envelope
	if err := json.Unmarshal(file, &env); err != nil || env.Format != Format {
		return nil, errors.New("this isn't a TrueDeploy backup file")
	}
	if env.Version != Version || env.KDF != "argon2id" {
		return nil, fmt.Errorf("this backup was made by a newer TrueDeploy (format %d): update the app to import it", env.Version)
	}
	// Refuse absurd parameters instead of hanging or exhausting memory.
	if env.Time < 1 || env.Time > 20 || env.Memory < 8*1024 || env.Memory > 1024*1024 || env.Threads < 1 || len(env.Salt) < 16 || len(env.Nonce) != 12 {
		return nil, errors.New("the backup file's header is invalid")
	}
	gcm, err := newGCM(key(pass, env.Salt, env.Time, env.Memory, env.Threads))
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, env.Nonce, env.Data, header(env))
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	return plain, nil
}

func header(e envelope) []byte {
	e.Data = nil
	b, _ := json.Marshal(e)
	return b
}

func newGCM(k []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
