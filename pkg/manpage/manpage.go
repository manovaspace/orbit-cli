package manpage

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	SystemManDir = "/usr/local/share/man/man1"
	UserManDir   = "~/.local/share/man/man1"
)

// GenerateManPages generates a single unified, comprehensive man page 'manova.1' and 'm.1' symlink.
// It also cleans up any legacy fragmented 'manova-*.1' files in targetDir.
func GenerateManPages(rootCmd *cobra.Command, targetDir string) ([]string, error) {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create man directory %q: %w", targetDir, err)
	}

	// Purge legacy fragmented subpage files
	entries, _ := os.ReadDir(targetDir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "manova-") && strings.HasSuffix(name, ".1") {
			_ = os.Remove(filepath.Join(targetDir, name))
		}
	}

	content := GenerateRoffContent(rootCmd)
	manova1 := filepath.Join(targetDir, "manova.1")
	if err := os.WriteFile(manova1, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write manova.1: %w", err)
	}

	// Create m.1 symlink pointing to manova.1 (or copy if symlink fails)
	m1 := filepath.Join(targetDir, "m.1")
	_ = os.Remove(m1)
	if err := os.Symlink("manova.1", m1); err != nil {
		_ = os.WriteFile(m1, []byte(content), 0644)
	}

	return []string{manova1, m1}, nil
}

// GenerateRoffContent builds a complete, standard roff man page from the Cobra command tree.
func GenerateRoffContent(rootCmd *cobra.Command) string {
	var buf bytes.Buffer
	dateStr := time.Now().Format("02-Jan-2006")

	buf.WriteString(fmt.Sprintf(".TH \"MANOVA\" \"1\" \"%s\" \"Manova Orbit Platform\" \"Manova CLI Developer Reference\"\n", dateStr))
	buf.WriteString(".SH NAME\n")
	buf.WriteString("manova, m \\- Zero-leak developer onboarding, multi-repo sync, and dev stack orchestrator\n\n")

	buf.WriteString(".SH SYNOPSIS\n")
	buf.WriteString(".B manova\n")
	buf.WriteString("[\\fIcommand\\fR] [\\fIoptions\\fR] [\\fIarguments\\fR]\n.br\n")
	buf.WriteString(".B m\n")
	buf.WriteString("[\\fIcommand\\fR] [\\fIoptions\\fR] [\\fIarguments\\fR]\n\n")

	buf.WriteString(".SH DESCRIPTION\n")
	buf.WriteString("\\fBmanova\\fR (short alias \\fBm\\fR) is a high-performance developer workspace orchestrator and zero-leak onboarding engine. ")
	buf.WriteString("It coordinates developer identity (LLDAP), Forgejo Git repositories, WireGuard mesh VPN peers, 50-port block allocations, multi-repo manifests, local container stacks, and automated background update feeds.\n\n")

	// Group commands
	coreCmds := []*cobra.Command{}
	workspaceCmds := []*cobra.Command{}
	systemCmds := []*cobra.Command{}
	otherCmds := []*cobra.Command{}

	for _, c := range rootCmd.Commands() {
		if c.Hidden {
			continue
		}
		switch c.GroupID {
		case "core":
			coreCmds = append(coreCmds, c)
		case "workspace":
			workspaceCmds = append(workspaceCmds, c)
		case "system":
			systemCmds = append(systemCmds, c)
		default:
			if c.Name() != "help" && c.Name() != "completion" {
				otherCmds = append(otherCmds, c)
			}
		}
	}

	if len(coreCmds) > 0 {
		buf.WriteString(".SH CORE COMMANDS\n")
		writeCommandSection(&buf, coreCmds)
	}

	if len(workspaceCmds) > 0 {
		buf.WriteString(".SH WORKSPACE COMMANDS\n")
		writeCommandSection(&buf, workspaceCmds)
	}

	if len(systemCmds) > 0 {
		buf.WriteString(".SH SYSTEM & TOOLING\n")
		writeCommandSection(&buf, systemCmds)
	}

	if len(otherCmds) > 0 {
		buf.WriteString(".SH ADDITIONAL COMMANDS\n")
		writeCommandSection(&buf, otherCmds)
	}

	// Global Flags
	buf.WriteString(".SH GLOBAL OPTIONS\n")
	rootCmd.Flags().VisitAll(func(f *pflag.Flag) {
		writeFlagRoff(&buf, f)
	})

	// Environment Variables
	buf.WriteString(".SH ENVIRONMENT VARIABLES\n")
	buf.WriteString(".TP\n.B MANOVA_VERSION\nOverrides the target version downloaded by the installation script.\n")
	buf.WriteString(".TP\n.B MANOVA_FORCE_DETACHED\nForces detached background worker daemon mode instead of systemd user timers.\n")
	buf.WriteString(".TP\n.B MANOVA_INVITE_SECRET\nHMAC-SHA256 secret key used for signing and validating developer onboarding tokens.\n\n")

	// Files
	buf.WriteString(".SH FILES\n")
	buf.WriteString(".TP\n.B ~/.manova/feed.json\nCached edge update feed and broadcast messages.\n")
	buf.WriteString(".TP\n.B ~/.manova/users.json\nLocal developer identity directory and subsystem credentials.\n")
	buf.WriteString(".TP\n.B ~/.manova/messages.json\nSeen tracking store for passive notification banners.\n")
	buf.WriteString(".TP\n.B ~/.manova/state.json\nCLI execution metadata and post-update migration state.\n\n")

	// Examples
	buf.WriteString(".SH EXAMPLES\n")
	buf.WriteString(".TP\nClaim onboarding invite and provision local workspace:\n\\fBm onboard --token manova-inv...\\fR\n")
	buf.WriteString(".TP\nStart local container stack with Traefik/Caddy routing:\n\\fBm dev up\\fR\n")
	buf.WriteString(".TP\nList provisioned developer accounts across LDAP, Git, and VPN:\n\\fBm user list\\fR\n")
	buf.WriteString(".TP\nInspect recent release notes and highlights:\n\\fBm changelog\\fR\n")
	buf.WriteString(".TP\nUpdate CLI to latest stable release:\n\\fBm self-update\\fR\n\n")

	// Authors & Links
	buf.WriteString(".SH AUTHORS\n")
	buf.WriteString("Manova Space Platform Team (https://manova.space)\n\n")
	buf.WriteString(".SH REPORTING BUGS\n")
	buf.WriteString("GitHub Issues: https://github.com/manovaspace/orbit-cli/issues\n")

	return buf.String()
}

func writeCommandSection(buf *bytes.Buffer, cmds []*cobra.Command) {
	for _, c := range cmds {
		buf.WriteString(".TP\n")
		useLine := c.UseLine()
		if len(c.Aliases) > 0 {
			useLine += fmt.Sprintf(" (aliases: %s)", strings.Join(c.Aliases, ", "))
		}
		buf.WriteString(fmt.Sprintf("\\fB%s\\fR\n", useLine))

		desc := c.Long
		if desc == "" {
			desc = c.Short
		}
		buf.WriteString(fmt.Sprintf("%s\n", escapeRoff(desc)))

		// Subcommands if any
		subCmds := c.Commands()
		if len(subCmds) > 0 {
			buf.WriteString(".RS 4\n")
			buf.WriteString(".PP\n\\fBSubcommands:\\fR\n")
			for _, sub := range subCmds {
				if sub.Hidden {
					continue
				}
				buf.WriteString(fmt.Sprintf(".TP\n\\fB%s\\fR\n%s\n", sub.UseLine(), escapeRoff(sub.Short)))
			}
			buf.WriteString(".RE\n")
		}

		// Flags
		hasFlags := false
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if !hasFlags {
				buf.WriteString(".RS 4\n")
				buf.WriteString(".PP\n\\fBFlags:\\fR\n")
				hasFlags = true
			}
			writeFlagRoff(buf, f)
		})
		if hasFlags {
			buf.WriteString(".RE\n")
		}
		buf.WriteString("\n")
	}
}

func writeFlagRoff(buf *bytes.Buffer, f *pflag.Flag) {
	buf.WriteString(".TP\n")
	flagName := "--" + f.Name
	if f.Shorthand != "" {
		flagName = fmt.Sprintf("-%s, --%s", f.Shorthand, f.Name)
	}
	if f.Value.Type() != "bool" {
		flagName += fmt.Sprintf(" \\fI%s\\fR", f.Value.Type())
	}
	buf.WriteString(fmt.Sprintf("\\fB%s\\fR\n%s\n", flagName, escapeRoff(f.Usage)))
}

func escapeRoff(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "-", "\\-")
	return strings.TrimSpace(s)
}

// ResolveManDir resolves the best writable man page directory.
func ResolveManDir() string {
	if os.Geteuid() == 0 {
		return SystemManDir
	}
	if err := os.MkdirAll(SystemManDir, 0755); err == nil {
		testFile := filepath.Join(SystemManDir, ".test-write")
		if err := os.WriteFile(testFile, []byte(""), 0644); err == nil {
			_ = os.Remove(testFile)
			return SystemManDir
		}
	}

	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".local", "share", "man", "man1")
	}
	return "/tmp/man1"
}

// InstallManPages automatically resolves target directory and installs man pages.
func InstallManPages(cmd *cobra.Command) error {
	dir := ResolveManDir()
	return InstallToDir(cmd, dir)
}

// InstallToDir generates and writes man pages to targetDir and triggers mandb.
func InstallToDir(cmd *cobra.Command, targetDir string) error {
	if _, err := GenerateManPages(cmd, targetDir); err != nil {
		return err
	}
	if mandb, err := exec.LookPath("mandb"); err == nil {
		_ = exec.Command(mandb, "-q").Run()
	}
	return nil
}

// UninstallFromDir removes all manova*.1 and m.1 files from targetDir.
func UninstallFromDir(targetDir string) error {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		name := e.Name()
		if (strings.HasPrefix(name, "manova") || name == "m.1") && strings.HasSuffix(name, ".1") {
			_ = os.Remove(filepath.Join(targetDir, name))
		}
	}
	return nil
}

// UninstallManPages cleans up man pages from system and user directories.
func UninstallManPages() error {
	_ = UninstallFromDir(SystemManDir)
	if home, err := os.UserHomeDir(); err == nil {
		_ = UninstallFromDir(filepath.Join(home, ".local", "share", "man", "man1"))
	}
	return nil
}
