package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/manovaspace/orbit-cli/pkg/clierr"
	"github.com/manovaspace/orbit-cli/pkg/dev/application"
	"github.com/manovaspace/orbit-cli/pkg/dev/domain"
	"github.com/manovaspace/orbit-cli/pkg/dev/infrastructure/attached"
	"github.com/manovaspace/orbit-cli/pkg/output"
	"github.com/manovaspace/orbit-cli/pkg/render"
	"github.com/spf13/cobra"
)

// devAttached runs the foreground ssh tunnel with the terminal attached; command
// tests replace it with a fake.
var devAttached application.Attached = attached.New(os.Stdin, os.Stdout, os.Stderr)

func newDevTunnelCmd() *cobra.Command {
	var (
		hostFlag      string
		sshPortFlag   int
		identityFlag  string
		only          []string
		printFlag     bool
		sshConfigFlag bool
		authKeyFlag   string
		jsonFlag      bool
		rootFlag      string
	)

	cmd := &cobra.Command{
		Use:   "tunnel",
		Short: "Forward the shared dev services over SSH to 127.0.0.1",
		Long: "Forward every registry service and exception with runs: shared (ports.yaml) from the dev tunnel host to 127.0.0.1 " +
			"with ssh -L. The host comes from --host or dev.tunnel_host, the SSH port from --ssh-port or dev.tunnel_port. " +
			"The command runs in the foreground; Ctrl-C ends it. --print shows the ssh command, --ssh-config prints a Host block " +
			"for ~/.ssh/config, --authorized-key prints the server-side authorized_keys line for a public key file, and --json prints the plan.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			reg, registryPath, err := loadPortRegistry(rootFlag)
			if err != nil {
				return render.EmitFailure(out, jsonFlag, err)
			}
			if reg == nil {
				return render.EmitFailure(out, jsonFlag, clierr.Unverified(fmt.Sprintf("no port registry at %s; nothing to tunnel", registryPath)))
			}

			modes := 0
			for _, on := range []bool{printFlag, sshConfigFlag, authKeyFlag != "", jsonFlag} {
				if on {
					modes++
				}
			}
			if modes > 1 {
				return render.EmitFailure(out, jsonFlag, clierr.Usage("choose one of --print, --ssh-config, --authorized-key or --json"))
			}

			svc := &application.TunnelService{Attached: devAttached, Files: devTextFiles}

			if authKeyFlag != "" {
				forwards, err := application.SelectForwards(reg, only)
				if err != nil {
					return render.EmitFailure(out, false, err)
				}
				line, err := svc.AuthorizedKeyLine(forwards, authKeyFlag)
				if err != nil {
					return render.EmitFailure(out, false, err)
				}
				_, err = io.WriteString(out, line+"\n")
				return err
			}

			cfg, err := newConfigService(io.Discard).Show(getConfigPath(cmd))
			if err != nil {
				return render.EmitFailure(out, jsonFlag, err)
			}
			host := hostFlag
			if host == "" {
				host = cfg.Dev.TunnelHost
			}
			port := cfg.Dev.TunnelPort
			if cmd.Flags().Changed("ssh-port") {
				port = sshPortFlag
			}
			identity := identityFlag
			if identity == "" {
				identity = cfg.Dev.TunnelIdentity
			}
			home, _ := os.UserHomeDir()
			identity = domain.ExpandHome(identity, home)

			plan, err := application.PlanTunnel(application.TunnelInput{
				Registry: reg,
				Host:     host,
				Port:     port,
				Identity: identity,
				Only:     only,
			})
			if err != nil {
				return render.EmitFailure(out, jsonFlag, err)
			}

			switch {
			case jsonFlag:
				return output.Write(out, output.Success(plan))
			case printFlag:
				_, err := io.WriteString(out, strings.Join(plan.Command, " ")+"\n")
				return err
			case sshConfigFlag:
				text, err := svc.SSHConfig(plan)
				if err != nil {
					return render.EmitFailure(out, false, err)
				}
				_, err = io.WriteString(out, text)
				return err
			default:
				return svc.Run(cmd.Context(), plan)
			}
		},
	}

	cmd.Flags().StringVar(&hostFlag, "host", "", "SSH destination user@host (default: dev.tunnel_host)")
	cmd.Flags().IntVar(&sshPortFlag, "ssh-port", 0, "SSH port of the tunnel host (default: dev.tunnel_port)")
	cmd.Flags().StringVar(&identityFlag, "identity", "", "private key file for the tunnel, passed as ssh -i (default: dev.tunnel_identity; ~ expands to your home)")
	cmd.Flags().StringSliceVar(&only, "only", nil, "forward only these registry service names, comma separated")
	cmd.Flags().BoolVar(&printFlag, "print", false, "print the ssh command and exit")
	cmd.Flags().BoolVar(&sshConfigFlag, "ssh-config", false, "print a Host orbit-dev block for ~/.ssh/config and exit")
	cmd.Flags().StringVar(&authKeyFlag, "authorized-key", "", "print the server authorized_keys line for this ssh public key file (.pub)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "print the tunnel plan (host, port, forwards, command) as a JSON envelope and do not run ssh")
	cmd.Flags().StringVar(&rootFlag, "root", "", "workspace root holding handbook/agents/workspace/ports.yaml (default: the workspace containing the current directory)")
	return cmd
}
