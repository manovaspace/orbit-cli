package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/istty"
	"github.com/manovaspace/orbit-cli/pkg/manifest"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/manovaspace/orbit-cli/pkg/setup"
	"github.com/manovaspace/orbit-cli/pkg/setup/engine"
	"github.com/manovaspace/orbit-cli/pkg/setup/parts/toolchains"
	"github.com/manovaspace/orbit-cli/pkg/setup/spec"
	"github.com/manovaspace/orbit-cli/pkg/table"
	"github.com/spf13/cobra"
)

// defaultParts returns the setup parts in run order; command tests replace it with fakes.
var defaultParts = setup.Default

// stdinIsTerminal reports whether stdin is a terminal; command tests replace it.
var stdinIsTerminal = istty.IsInteractiveSession

// currentEUID returns the effective user ID; command tests replace it.
var currentEUID = os.Geteuid

const (
	setupProjectsUsage = "choose projects: o setup --projects falak"
	setupRootUsage     = "do not run o setup as root; create a non-root user with sudo and run as that user"
)

func refuseRoot() error {
	if currentEUID() == 0 {
		return clierr.Usage(setupRootUsage)
	}
	return nil
}

// setupDoneMessage names the rc file of the login shell that the toolchains part writes.
func setupDoneMessage(shell string) string {
	rc := toolchains.PrimaryRC(shell)
	if rc == "" {
		return "Setup complete. Open a new terminal so PATH changes apply, then run: o up"
	}
	return "Setup complete. Open a new terminal (or run: source ~/" + rc + ") so PATH changes apply, then run: o up"
}

// setupFlags are the flags of o setup and its o init alias. add and remove use only yes and json.
type setupFlags struct {
	projects    []string
	services    string
	gitProtocol string
	workspace   string
	plan        bool
	yes         bool
	json        bool
}

// setupNonInteractive reports whether this run must not prompt: CI=true or stdin is not a terminal.
func setupNonInteractive() bool {
	return os.Getenv("CI") == "true" || !stdinIsTerminal()
}

// setupSession is one run of o setup, o add or o remove. Prompts share one reader so buffered
// input is never lost between questions. In JSON mode prompts go to stderr so stdout stays one envelope.
type setupSession struct {
	cmd            *cobra.Command
	flags          *setupFlags
	nonInteractive bool
	reader         *bufio.Reader
	prompt         io.Writer
}

func newSetupSession(cmd *cobra.Command, f *setupFlags) *setupSession {
	prompt := cmd.OutOrStdout()
	if f.json {
		prompt = cmd.ErrOrStderr()
	}
	return &setupSession{
		cmd:            cmd,
		flags:          f,
		nonInteractive: setupNonInteractive(),
		reader:         bufio.NewReader(cmd.InOrStdin()),
		prompt:         prompt,
	}
}

// confirm is the engine's Confirm. It renders the preview and asks "Apply these changes?" unless
// the run is non-interactive or --yes is set. An empty answer, y or yes approves; end of input declines.
func (s *setupSession) confirm(preview []engine.PartResult) bool {
	if s.nonInteractive || s.flags.yes {
		return true
	}
	engine.RenderPlan(render.NewPrinter(s.prompt), engine.Result{Parts: preview})
	fmt.Fprint(s.prompt, "Apply these changes? [Y/n] ")
	line, err := s.reader.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(s.prompt)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	}
	return false
}

// apply runs parts with the session's confirmation.
func (s *setupSession) apply(st *engine.State, parts []engine.Part) (engine.Result, error) {
	e := engine.Engine{Parts: parts, Confirm: s.confirm}
	return e.Apply(s.cmd.Context(), st)
}

// emit writes one apply result: text sections and summary, then extra lines and done on success;
// or one JSON envelope. The returned error is the apply error.
func (s *setupSession) emit(res engine.Result, err error, extra func(*render.Printer), done string) error {
	return render.EmitFindings(s.cmd.OutOrStdout(), s.flags.json, res, err, func(p *render.Printer) error {
		engine.RenderResult(p, res)
		if extra != nil {
			extra(p)
		}
		if err == nil && done != "" {
			p.Text(done)
		}
		return nil
	})
}

func newSetupCmd() *cobra.Command {
	f := &setupFlags{}
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up this machine for the projects you choose",
		Long: `Records the machine choices (projects, services, git protocol, workspace) in machine.yaml,
then clones the chosen repositories and prepares their toolchains, services and env files.
Without --projects an interactive run asks which projects to use; a non-interactive run
(CI=true or stdin not a terminal) must be given --projects.
Run 'o doctor' to check the setup without changing anything.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd, f)
		},
	}
	bindSetupFlags(cmd, f)
	return cmd
}

// bindSetupFlags registers the o setup flags on cmd.
func bindSetupFlags(cmd *cobra.Command, f *setupFlags) {
	cmd.Flags().StringSliceVar(&f.projects, "projects", nil, "Projects to set up, comma-separated; replaces the saved list")
	cmd.Flags().StringVar(&f.services, "services", "", "Local services: native, container or external (saved)")
	cmd.Flags().StringVar(&f.gitProtocol, "git-protocol", "", "Git protocol for clones: https or ssh (saved)")
	cmd.Flags().StringVar(&f.workspace, "workspace", "", "Workspace directory, absolute or ~/ (saved)")
	cmd.Flags().BoolVar(&f.plan, "plan", false, "Show what would change; change nothing and save nothing")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "Apply without asking to confirm")
	cmd.Flags().BoolVar(&f.json, "json", false, "Print the result as a JSON envelope")
}

func runSetup(cmd *cobra.Command, f *setupFlags) error {
	if err := refuseRoot(); err != nil {
		return err
	}
	s := newSetupSession(cmd, f)
	m, err := setupMachine(cmd, f)
	if err != nil {
		return err
	}
	if f.plan {
		return s.plan(m)
	}
	if len(m.Projects) == 0 && (s.nonInteractive || f.yes) {
		return clierr.Usage(setupProjectsUsage)
	}

	st, err := stateFor(m, s.nonInteractive)
	if err != nil {
		return err
	}
	if st.Manifest != nil && len(m.Projects) > 0 {
		if err := checkProjectNames(st.Manifest, m.Projects); err != nil {
			return err
		}
	}
	if setupFlagsChanged(cmd) {
		// Flags are saved before the handbook phase so a failed run keeps them.
		// Project names wait for the manifest: until it exists the saved list is kept.
		keep := m
		if st.Manifest == nil {
			prev, _, err := spec.LoadMachine(machinePath())
			if err != nil {
				return err
			}
			keep.Projects = prev.Projects
		}
		if err := spec.SaveMachine(machinePath(), keep); err != nil {
			return err
		}
	}
	if st.Manifest == nil || len(m.Projects) == 0 {
		// Phase one: the handbook provides the project list, so it runs before the prompt.
		res, err := s.apply(st, filterParts(defaultParts(), "system", "identity", "handbook"))
		if err != nil {
			return s.emit(res, err, nil, "")
		}
		if !f.json {
			if err := s.emit(res, nil, nil, ""); err != nil {
				return err
			}
		}
		if err := st.Reload(); err != nil {
			return err
		}
		if st.Manifest == nil {
			return clierr.Failed("the workspace manifest is missing after the handbook phase")
		}
		if len(m.Projects) == 0 {
			chosen, err := s.chooseProjects(st.Manifest)
			if err != nil {
				return err
			}
			m.Projects = chosen
		}
		if err := checkProjectNames(st.Manifest, m.Projects); err != nil {
			return err
		}
	}

	if err := spec.SaveMachine(machinePath(), m); err != nil {
		return err
	}
	st.Machine = m
	res, err := s.apply(st, defaultParts())
	shell := toolchains.LoginShell(cmd.Context(), st.Run, st.Platform, os.Getenv("SHELL"))
	return s.emit(res, err, nil, setupDoneMessage(shell))
}

// setupFlagsChanged reports whether a flag that overwrites the machine file was given.
func setupFlagsChanged(cmd *cobra.Command) bool {
	for _, name := range []string{"projects", "services", "git-protocol", "workspace"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// setupMachine loads machine.yaml, overwrites the fields whose flags were set and validates the result.
// Nothing is saved here.
func setupMachine(cmd *cobra.Command, f *setupFlags) (spec.Machine, error) {
	m, _, err := spec.LoadMachine(machinePath())
	if err != nil {
		return spec.Machine{}, err
	}
	if cmd.Flags().Changed("projects") {
		m.Projects = cleanNames(f.projects)
	}
	if cmd.Flags().Changed("services") {
		m.Services = f.services
	}
	if cmd.Flags().Changed("git-protocol") {
		m.GitProtocol = f.gitProtocol
	}
	if cmd.Flags().Changed("workspace") {
		ws, err := expandWorkspace(f.workspace)
		if err != nil {
			return spec.Machine{}, err
		}
		m.Workspace = ws
	}
	if err := m.Validate(); err != nil {
		return spec.Machine{}, err
	}
	return m, nil
}

// plan shows what o setup would do. It runs Engine.Check only and saves nothing.
func (s *setupSession) plan(m spec.Machine) error {
	st, err := stateFor(m, s.nonInteractive)
	if err != nil {
		return err
	}
	if st.Manifest != nil {
		if err := checkProjectNames(st.Manifest, m.Projects); err != nil {
			return err
		}
	}
	e := engine.Engine{Parts: defaultParts()}
	res := e.Check(s.cmd.Context(), st)
	var failure error
	if res.Failed > 0 {
		failure = clierr.Failed(fmt.Sprintf("setup check failed: %d part(s) failed", res.Failed))
	}
	return render.EmitFindings(s.cmd.OutOrStdout(), s.flags.json, res, failure, func(p *render.Printer) error {
		engine.RenderPlan(p, res)
		return nil
	})
}

// chooseProjects shows the numbered project list and reads the choice from the session reader.
func (s *setupSession) chooseProjects(mf *manifest.WorkspaceManifest) ([]string, error) {
	names := mf.ProjectNames()
	if len(names) == 0 {
		return nil, clierr.Failed("the workspace manifest declares no projects")
	}
	fmt.Fprintln(s.prompt, "Projects in the workspace:")
	for i, name := range names {
		line := fmt.Sprintf("  %d) %s", i+1, name)
		if d := mf.Projects[name].Description; d != "" {
			line += " - " + d
		}
		fmt.Fprintln(s.prompt, line)
	}
	fmt.Fprint(s.prompt, "Choose projects (numbers or names, comma or space separated): ")
	line, err := s.reader.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(s.prompt)
		return nil, clierr.Usage(setupProjectsUsage)
	}
	return parseProjectChoice(line, names)
}

// parseProjectChoice maps a comma or space separated answer of numbers and names to project names,
// in the order given, without duplicates.
func parseProjectChoice(input string, names []string) ([]string, error) {
	fields := strings.FieldsFunc(input, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	if len(fields) == 0 {
		return nil, clierr.Usage("choose at least one project; valid projects: " + strings.Join(names, ", "))
	}
	var chosen []string
	for _, tok := range fields {
		name := tok
		if n, err := strconv.Atoi(tok); err == nil {
			if n < 1 || n > len(names) {
				return nil, clierr.Usage(fmt.Sprintf("no project numbered %d; valid projects: %s", n, strings.Join(names, ", ")))
			}
			name = names[n-1]
		} else if !slices.Contains(names, tok) {
			return nil, unknownProject(tok, names)
		}
		if !slices.Contains(chosen, name) {
			chosen = append(chosen, name)
		}
	}
	return chosen, nil
}

// checkProjectNames refuses any project the manifest does not declare.
func checkProjectNames(mf *manifest.WorkspaceManifest, projects []string) error {
	names := mf.ProjectNames()
	for _, p := range projects {
		if !slices.Contains(names, p) {
			return unknownProject(p, names)
		}
	}
	return nil
}

func unknownProject(name string, names []string) error {
	return clierr.Usage(fmt.Sprintf("unknown project %q; valid projects: %s", name, strings.Join(names, ", ")))
}

// filterParts returns the parts of all whose names are listed, in their order in all.
func filterParts(all []engine.Part, names ...string) []engine.Part {
	var out []engine.Part
	for _, p := range all {
		if slices.Contains(names, p.Name()) {
			out = append(out, p)
		}
	}
	return out
}

// cleanNames trims, drops empty names and removes duplicates, keeping the first occurrence.
func cleanNames(in []string) []string {
	var out []string
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// expandWorkspace expands a leading ~ and cleans an absolute path. Relative paths are kept so
// Machine.Validate refuses them.
func expandWorkspace(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", clierr.Wrap(err, clierr.Failed("cannot resolve home directory"))
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if filepath.IsAbs(p) {
		p = filepath.Clean(p)
	}
	return p, nil
}

func newAddCmd() *cobra.Command {
	return newProjectChangeCmd(true)
}

func newRemoveCmd() *cobra.Command {
	return newProjectChangeCmd(false)
}

// newProjectChangeCmd builds o add (add true) or o remove (add false).
func newProjectChangeCmd(add bool) *cobra.Command {
	f := &setupFlags{}
	use, short := "remove <project>...", "Stop selecting projects; their repositories stay on disk"
	if add {
		use, short = "add <project>...", "Select projects and set up their repositories"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long: `Changes the chosen projects in machine.yaml and applies the setup.
Removing a project never deletes a directory: repositories that are cloned but no longer
selected are listed, and you remove them yourself.`,
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return changeProjects(cmd, f, args, add)
		},
	}
	cmd.Flags().BoolVar(&f.yes, "yes", false, "Apply without asking to confirm")
	cmd.Flags().BoolVar(&f.json, "json", false, "Print the result as a JSON envelope")
	return cmd
}

func changeProjects(cmd *cobra.Command, f *setupFlags, names []string, add bool) error {
	if err := refuseRoot(); err != nil {
		return err
	}
	s := newSetupSession(cmd, f)
	m, _, err := spec.LoadMachine(machinePath())
	if err != nil {
		return err
	}
	st, err := stateFor(m, s.nonInteractive)
	if err != nil {
		return err
	}
	if st.Manifest == nil {
		return clierr.Usage("run o setup first: the workspace manifest is not cloned yet")
	}
	valid := st.Manifest.ProjectNames()
	for _, n := range names {
		if !slices.Contains(valid, n) {
			return unknownProject(n, valid)
		}
	}

	var before []manifest.RepoTarget
	if !add {
		if before, err = st.Selected(); err != nil {
			return err
		}
	}
	projects := append([]string(nil), m.Projects...)
	for _, n := range names {
		if !add {
			projects = slices.DeleteFunc(projects, func(p string) bool { return p == n })
			continue
		}
		if !slices.Contains(projects, n) {
			projects = append(projects, n)
		}
	}
	m.Projects = projects
	if err := spec.SaveMachine(machinePath(), m); err != nil {
		return err
	}

	st.Machine = m
	var notSelected []string
	if !add {
		after, err := st.Selected()
		if err != nil {
			return err
		}
		for _, r := range before {
			if !repoListed(after, r.Path) && repoCloned(st, r.Path) {
				notSelected = append(notSelected, r.Path)
			}
		}
	}
	res, err := s.apply(st, defaultParts())
	var extra func(*render.Printer)
	if len(notSelected) > 0 {
		extra = func(p *render.Printer) {
			p.Line(render.StatusInfo, "no longer selected: "+strings.Join(notSelected, ", "))
		}
	}
	return s.emit(res, err, extra, setupDoneMessage(toolchains.LoginShell(cmd.Context(), st.Run, st.Platform, os.Getenv("SHELL"))))
}

func repoListed(targets []manifest.RepoTarget, path string) bool {
	for _, t := range targets {
		if t.Path == path {
			return true
		}
	}
	return false
}

// repoCloned reports whether the workspace path holds a git repository (has .git/HEAD).
func repoCloned(s *engine.State, path string) bool {
	_, err := s.FS.Stat(filepath.Join(s.Root, path, ".git", "HEAD"))
	return err == nil
}

func newProjectsCmd() *cobra.Command {
	var jsonFlag bool
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "List the workspace projects, whether you chose them and whether their repos are cloned",
		Long: `State is cloned when every repository of the project has a .git/HEAD on disk, not cloned when none
does, and partial otherwise. Nothing is fetched from the network.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, _, err := spec.LoadMachine(machinePath())
			if err != nil {
				return err
			}
			st, err := stateFor(m, true)
			if err != nil {
				return err
			}
			if st.Manifest == nil {
				return clierr.Unverified("the workspace manifest is not cloned yet; run o setup first")
			}
			rows, err := projectRows(st)
			if err != nil {
				return err
			}
			return render.Emit(cmd.OutOrStdout(), jsonFlag, rows, func(p *render.Printer) error {
				if len(rows) == 0 {
					p.Empty("project")
					return nil
				}
				tbl := table.New(
					table.Column{Title: "PROJECT", HeaderStyle: render.Header, CellStyle: render.Bold, MinWidth: 12},
					table.Column{Title: "CHOSEN", HeaderStyle: render.Header, MinWidth: 6},
					table.Column{Title: "STATE", HeaderStyle: render.Header, MinWidth: 10},
					table.Column{Title: "REPOS", HeaderStyle: render.Header, CellStyle: render.Subtle, MinWidth: 20, Flexible: true},
				)
				for _, r := range rows {
					tbl.AddRow(r.Name, yesNo(r.Chosen), r.State, strings.Join(r.Repos, ", "))
				}
				return p.Table(tbl)
			})
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the result as a JSON envelope")
	return cmd
}

// projectRow is one line of o projects.
type projectRow struct {
	Name   string   `json:"name"`
	Chosen bool     `json:"chosen"`
	Repos  []string `json:"repos"`
	State  string   `json:"state"`
}

func projectRows(st *engine.State) ([]projectRow, error) {
	rows := make([]projectRow, 0)
	for _, name := range st.Manifest.ProjectNames() {
		repos, err := st.Manifest.ProjectRepos(name)
		if err != nil {
			return nil, err
		}
		row := projectRow{Name: name, Chosen: slices.Contains(st.Machine.Projects, name), Repos: []string{}}
		cloned := 0
		for _, r := range repos {
			row.Repos = append(row.Repos, r.Path)
			if repoCloned(st, r.Path) {
				cloned++
			}
		}
		switch {
		case cloned == len(repos):
			row.State = "cloned"
		case cloned == 0:
			row.State = "not cloned"
		default:
			row.State = "partial"
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
