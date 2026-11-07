package backupfile

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSealOpen(t *testing.T) {
	plain := []byte(`{"servers":{"s1":{"password":"hunter2"}}}`)
	file, err := Seal(plain, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(file, []byte("hunter2")) || bytes.Contains(file, []byte("servers")) {
		t.Fatal("the file must not contain the plaintext")
	}
	got, err := Open(file, "correct horse battery")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %v %q", err, got)
	}
	if _, err := Open(file, "wrong horse battery"); err != ErrWrongPassphrase {
		t.Errorf("wrong passphrase: %v", err)
	}
	other, _ := Seal(plain, "correct horse battery")
	if bytes.Equal(file, other) {
		t.Error("salt and nonce must be random")
	}
}

func TestTamperingIsDetected(t *testing.T) {
	file, _ := Seal([]byte("data"), "a long passphrase")
	var env map[string]any
	_ = json.Unmarshal(file, &env)

	// Weaker KDF settings in the header must not decrypt (header is authenticated).
	env["time"] = 1
	b, _ := json.Marshal(env)
	if _, err := Open(b, "a long passphrase"); err != ErrWrongPassphrase {
		t.Errorf("tampered header: %v", err)
	}
	env["time"] = 3
	env["memoryKiB"] = 1 << 30 // memory bomb
	b, _ = json.Marshal(env)
	if _, err := Open(b, "a long passphrase"); err == nil || !strings.Contains(err.Error(), "header is invalid") {
		t.Errorf("absurd memory: %v", err)
	}
	if _, err := Open([]byte(`{"hello":1}`), "x"); err == nil || !strings.Contains(err.Error(), "isn't a TrueDeploy backup") {
		t.Errorf("other file: %v", err)
	}
	env["memoryKiB"] = 65536
	env["version"] = 9
	b, _ = json.Marshal(env)
	if _, err := Open(b, "a long passphrase"); err == nil || !strings.Contains(err.Error(), "newer TrueDeploy") {
		t.Errorf("future version: %v", err)
	}
	if _, err := Seal([]byte("x"), "short"); err == nil {
		t.Error("short passphrase accepted")
	}
}
