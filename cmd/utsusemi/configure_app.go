package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/app"
	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/target"
)

func newConfigureAppCmd() *cobra.Command {
	var (
		brokerURL   string
		org         string
		runnerGroup int64
		outputPath  string
		force       bool
		opts        runnerOptions
	)

	cmd := &cobra.Command{
		Use:     "app",
		Short:   "Configure with the Utsusemi GitHub App",
		Example: `  utsusemi configure app --org my-org`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.ValidateBrokerURL(brokerURL); err != nil {
				return err
			}
			if err := confirmConfigOverwrite(outputPath, force, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
				return err
			}

			tgt, err := target.FromConfig(config.TargetYAML(org, "", runnerGroup))
			if err != nil {
				return err
			}

			flowClient := &hostcredential.DeviceFlowClient{}
			flow, err := flowClient.Authorize(cmd.Context(), hostcredential.PublicAppClientID, hostcredential.DeviceFlowPrompt{
				WriteUserCode: func(userCode, verificationURI string) {
					fmt.Fprintf(cmd.OutOrStdout(), "GitHub device code: %s\n", userCode)
					fmt.Fprintf(cmd.OutOrStdout(), "Verification URL: %s\n", verificationURI)
				},
				OpenBrowser: func(verificationURI string) error {
					return promptAndOpenBrowser(cmd.InOrStdin(), cmd.OutOrStdout(), verificationURI)
				},
			})
			if err != nil {
				return err
			}

			githubUser, err := hostcredential.FetchGitHubUserLogin(cmd.Context(), http.DefaultClient, flow.AccessToken)
			if err != nil {
				return fmt.Errorf("lookup github user: %w", err)
			}

			hostJWT, confirmedTarget, err := hostcredential.ExchangeHostJWT(
				cmd.Context(),
				http.DefaultClient,
				brokerURL,
				flow.AccessToken,
				tgt,
			)
			if err != nil {
				return fmt.Errorf("configure app exchange failed: %w", err)
			}

			credential, err := hostcredential.NewBundle(hostJWT, flow.RefreshToken, githubUser)
			if err != nil {
				return err
			}

			cfg := &config.Config{
				Target: config.TargetYAML(confirmedTarget.Org, "", confirmedTarget.RunnerGroupID),
				Registration: config.Registration{
					Mode:      config.ModeHostedApp,
					BrokerURL: brokerURL,
				},
			}
			opts.apply(cfg)
			config.ApplyDefaults(cfg)
			if _, err := app.ValidateConfig(cfg); err != nil {
				return err
			}

			if err := saveCredential(cfg, credential); err != nil {
				return err
			}
			if err := writeConfig(outputPath, cfg); err != nil {
				return err
			}
			printConfigureSuccess(outputPath, githubUser)
			return nil
		},
	}

	cmd.Flags().StringVar(&brokerURL, "broker", config.DefaultHostedAppBrokerURL, "broker base URL")
	cmd.Flags().StringVar(&org, "org", "", "GitHub organization")
	cmd.Flags().Int64Var(&runnerGroup, "runner-group-id", 1, "runner group id for org target")
	cmd.Flags().StringVar(&outputPath, "output", configPath, "config output path")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config without prompting")
	addRunnerFlags(cmd, &opts)
	return cmd
}

func openBrowser(rawURL string) error {
	return exec.Command("open", rawURL).Start()
}

func promptAndOpenBrowser(in io.Reader, out io.Writer, rawURL string) error {
	inFile, inputIsFile := in.(*os.File)
	outFile, outputIsFile := out.(*os.File)
	if !inputIsFile || !outputIsFile || !isTerminal(inFile) || !isTerminal(outFile) {
		return nil
	}

	fmt.Fprint(out, "Copy the code, then press Enter to open GitHub in your browser: ")
	if _, err := bufio.NewReader(in).ReadString('\n'); err != nil && err != io.EOF {
		return fmt.Errorf("wait for browser confirmation: %w", err)
	}
	if err := openBrowser(rawURL); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
