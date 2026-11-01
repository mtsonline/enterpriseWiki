//go:build unix

package macro_shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
	"time"
	"unicode/utf8"
)

// limitedBuffer keeps the first MaxOutput bytes.
type limitedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := MaxOutput - l.buf.Len(); room > 0 {
		if len(p) > room {
			l.buf.Write(p[:room])
			l.truncated = true
		} else {
			l.buf.Write(p)
		}
	} else if len(p) > 0 {
		l.truncated = true
	}
	return len(p), nil
}

// run executes cmd with /bin/sh in a fresh temporary directory, a minimal
// environment and its own process group (killed on timeout). When the wiki
// runs as root the command runs as the unprivileged user (default nobody).
func run(ctx context.Context, cmd string, timeout time.Duration, runAs string) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "ewiki-shell-*")
	if err != nil {
		return "", -1, err
	}
	defer os.RemoveAll(dir)
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	c.Dir = dir
	c.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "HOME=" + dir, "TMPDIR=" + dir}
	attr := &syscall.SysProcAttr{Setpgid: true}
	if os.Geteuid() == 0 {
		if runAs == "" {
			runAs = "nobody"
		}
		u, err := user.Lookup(runAs)
		if err != nil {
			return "", -1, fmt.Errorf("shell user %q not found", runAs)
		}
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		if uid == 0 {
			return "", -1, errors.New("refusing to run shell commands as root (set plugins.macros.shell_user)")
		}
		attr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}
		_ = os.Chown(dir, int(uid), int(gid))
	}
	c.SysProcAttr = attr
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 2 * time.Second
	var out limitedBuffer
	c.Stdout, c.Stderr = &out, &out
	err = c.Run()
	text := out.buf.String()
	for !utf8.ValidString(text) && len(text) > 0 { // cut a split multi-byte rune
		text = text[:len(text)-1]
	}
	if out.truncated {
		text += "\n[… output truncated]"
	}
	if ctx.Err() == context.DeadlineExceeded {
		return "", -1, fmt.Errorf("timeout after %s", timeout)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return text, ee.ExitCode(), nil
	}
	if err != nil {
		return "", -1, err
	}
	return text, 0, nil
}
