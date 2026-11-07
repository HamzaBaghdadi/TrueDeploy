package sshx

import (
	"errors"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Shell is an interactive login shell on a pseudo-terminal.
type Shell struct {
	session *ssh.Session
	stdin   io.WriteCloser
	done    chan struct{}
	err     error
	once    sync.Once
}

// OpenShell starts a login shell with a PTY of the given size. Output (stdout
// and stderr, merged by the PTY) is passed to onData from a background
// goroutine until the shell exits.
func (c *Client) OpenShell(cols, rows int, onData func([]byte)) (*Shell, error) {
	return c.OpenCommand(cols, rows, "", onData)
}

// OpenCommand is OpenShell running command (on a PTY) instead of the login
// shell; "" means the login shell.
func (c *Client) OpenCommand(cols, rows int, command string, onData func([]byte)) (*Shell, error) {
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	s, err := c.NewSession()
	if err != nil {
		return nil, err
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}
	if err := s.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		s.Close()
		return nil, err
	}
	stdin, err := s.StdinPipe()
	if err != nil {
		s.Close()
		return nil, err
	}
	stdout, err := s.StdoutPipe()
	if err != nil {
		s.Close()
		return nil, err
	}
	s.Stderr = s.Stdout // unused with a PTY, but keeps stray stderr visible
	start := s.Shell
	if command != "" {
		start = func() error { return s.Start(command) }
	}
	if err := start(); err != nil {
		s.Close()
		return nil, err
	}

	sh := &Shell{session: s, stdin: stdin, done: make(chan struct{})}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				onData(chunk)
			}
			if err != nil {
				break
			}
		}
		werr := s.Wait()
		var exit *ssh.ExitError
		var missing *ssh.ExitMissingError
		if werr != nil && !errors.As(werr, &exit) && !errors.As(werr, &missing) && !errors.Is(werr, io.EOF) {
			sh.err = werr
		}
		close(sh.done)
	}()
	return sh, nil
}

// Write sends keystrokes (or pasted text) to the shell.
func (sh *Shell) Write(p []byte) error {
	_, err := sh.stdin.Write(p)
	return err
}

// Resize tells the remote PTY the terminal's new size.
func (sh *Shell) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return sh.session.WindowChange(rows, cols)
}

// Done is closed when the shell exits.
func (sh *Shell) Done() <-chan struct{} { return sh.done }

// Err is the abnormal exit error, if any, once Done is closed.
func (sh *Shell) Err() error { return sh.err }

// Close ends the shell.
func (sh *Shell) Close() {
	sh.once.Do(func() {
		_ = sh.stdin.Close()
		_ = sh.session.Close()
	})
}
