package services

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

// mailSMTPPort is where Mailpit accepts mail; mailUIPort serves its web interface.
const (
	mailSMTPPort = 10024
	mailUIPort   = 10025
)

// mailpit is the native Mailpit mail catcher, started in the background and tracked by a PID file.
type mailpit struct {
	run      runner.Runner
	fs       engine.FS
	stateDir string
	dial     dialer
	alive    func(pid int) bool
	kill     func(pid int, sig syscall.Signal) error
}

func (m *mailpit) Name() string { return "mailpit" }

func (m *mailpit) pidFile() string { return filepath.Join(m.stateDir, "mailpit.pid") }

func (m *mailpit) logFile() string { return filepath.Join(m.stateDir, "mailpit.log") }

// readPID returns the PID in the PID file. ok is false when the file is absent or unreadable.
func (m *mailpit) readPID() (int, bool) {
	data, err := m.fs.ReadFile(m.pidFile())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// Status reports orbit ownership when the PID file names a live process. Otherwise a TCP probe of
// the SMTP port answers for a foreign listener (owner other).
func (m *mailpit) Status(ctx context.Context) (Status, error) {
	if pid, ok := m.readPID(); ok && m.alive(pid) {
		return Status{Running: true, Port: mailSMTPPort, Owner: "orbit"}, nil
	}
	if m.dial(fmt.Sprintf("127.0.0.1:%d", mailSMTPPort), dialTimeout) == nil {
		return Status{Running: true, Port: mailSMTPPort, Owner: "other"}, nil
	}
	return Status{Port: mailSMTPPort}, nil
}

// Start launches mailpit with nohup, its output going to the log file, and records the PID the
// shell reports.
func (m *mailpit) Start(ctx context.Context) error {
	if err := m.fs.MkdirAll(m.stateDir, 0o700); err != nil {
		return clierr.Wrap(err, clierr.Failed("cannot create "+m.stateDir))
	}
	line := fmt.Sprintf("nohup mailpit --smtp 127.0.0.1:%d --listen 127.0.0.1:%d >%s 2>&1 & echo $!",
		mailSMTPPort, mailUIPort, shellQuote(m.logFile()))
	res, err := m.run.Run(ctx, "", "sh", "-c", line)
	if err != nil {
		return clierr.Wrap(err, clierr.Tool("cannot start mailpit"))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if err != nil || pid <= 0 {
		return clierr.Tool("mailpit did not report a process id")
	}
	return m.fs.WriteFile(m.pidFile(), []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

// Stop terminates the process named by the PID file when it is alive, then removes the file.
func (m *mailpit) Stop(ctx context.Context) error {
	pid, ok := m.readPID()
	if !ok {
		return nil
	}
	if m.alive(pid) {
		if err := m.kill(pid, syscall.SIGTERM); err != nil {
			return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot stop mailpit (pid %d)", pid)))
		}
	}
	if err := m.fs.Remove(m.pidFile()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return clierr.Wrap(err, clierr.Failed("cannot remove "+m.pidFile()))
	}
	return nil
}

// shellQuote wraps s in single quotes for sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
