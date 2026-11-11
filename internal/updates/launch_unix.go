//go:build !windows

package updates

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Launch writes the helper script to dir and starts it in its own session,
// so it keeps running after the app exits.
func Launch(script, dir string) error {
	path := filepath.Join(dir, "apply-update.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
