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
	"time"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/runner"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
)

// mailSMTPPort is where Mailpit accepts mail; mailUIPort serves its web interface.
const (
	mailSMTPPort = 10024
	mailUIPort   = 10025
)

// mailCommand is the command name a process must report to be treated as orbit's Mailpit.
const mailCommand = "mailpit"

// mailPollStep is the pause between probes while Start waits for the SMTP port.
const mailPollStep = 100 * time.Millisecond

// mailStartWait bounds how long Start waits for the SMTP port to answer. Tests shorten it.
var mailStartWait = 3 * time.Second

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

func (m *mailpit) smtpAddr() string { return fmt.Sprintf("127.0.0.1:%d", mailSMTPPort) }

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

// processName returns the command name ps reports for pid. A dead process is an error.
func (m *mailpit) processName(ctx context.Context, pid int) (string, error) {
	res, err := m.run.Run(ctx, "", "ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	if err != nil {
		return "", err
	}
	return filepath.Base(strings.TrimSpace(res.Stdout)), nil
}

// owned reports whether the PID file names a live process whose command is mailpit. Any other PID
// file (absent contents, dead process, foreign command, ps failure) is stale: it is removed and the
// caller must not signal anything. EPERM from the liveness probe counts as not alive.
func (m *mailpit) owned(ctx context.Context) (int, bool, error) {
	if pid, ok := m.readPID(); ok && m.alive(pid) {
		if name, err := m.processName(ctx, pid); err == nil && name == mailCommand {
			return pid, true, nil
		}
	}
	if err := m.fs.Remove(m.pidFile()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, false, clierr.Wrap(err, clierr.Failed("cannot remove "+m.pidFile()))
	}
	return 0, false, nil
}

// Status reports orbit ownership when the PID file names a verified mailpit process and the SMTP
// port answers. A listener without that ownership is another process (owner other).
func (m *mailpit) Status(ctx context.Context) (Status, error) {
	_, owned, err := m.owned(ctx)
	if err != nil {
		return Status{Port: mailSMTPPort}, err
	}
	listening := m.dial(m.smtpAddr(), dialTimeout) == nil
	switch {
	case owned && listening:
		return Status{Running: true, Port: mailSMTPPort, Owner: "orbit"}, nil
	case listening:
		return Status{Running: true, Port: mailSMTPPort, Owner: "other"}, nil
	}
	return Status{Port: mailSMTPPort}, nil
}

// Start launches mailpit with nohup, its output going to the log file. The PID file is written only
// once the SMTP port answers.
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
	if err := m.awaitListener(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return clierr.Tool(fmt.Sprintf("mailpit did not accept connections on 127.0.0.1:%d; see %s", mailSMTPPort, m.logFile()))
	}
	return m.fs.WriteFile(m.pidFile(), []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

// awaitListener probes the SMTP port every mailPollStep until it answers or mailStartWait passes.
func (m *mailpit) awaitListener(ctx context.Context) error {
	deadline := time.Now().Add(mailStartWait)
	for {
		if m.dial(m.smtpAddr(), dialTimeout) == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errors.New("no listener on the SMTP port")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(mailPollStep):
		}
	}
}

// Stop terminates the process named by the PID file only when it is verified as mailpit, then
// removes the file. A stale PID file is removed without any signal.
func (m *mailpit) Stop(ctx context.Context) error {
	pid, owned, err := m.owned(ctx)
	if err != nil || !owned {
		return err
	}
	if err := m.kill(pid, syscall.SIGTERM); err != nil {
		return clierr.Wrap(err, clierr.Failed(fmt.Sprintf("cannot stop mailpit (pid %d)", pid)))
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
