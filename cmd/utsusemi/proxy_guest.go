package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/proxyfwd"
)

type stdioRWC struct {
	in  *os.File
	out *os.File
}

func (s stdioRWC) Read(p []byte) (int, error)  { return s.in.Read(p) }
func (s stdioRWC) Write(p []byte) (int, error) { return s.out.Write(p) }
func (s stdioRWC) Close() error {
	_ = s.in.Close()
	_ = s.out.Close()
	return nil
}

func newProxyGuestCmd() *cobra.Command {
	var listen string
	cmd := &cobra.Command{
		Use:           "proxy-guest",
		Short:         "Serve guest loopback proxy tunnel over stdio (internal)",
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if listen == "" {
				return fmt.Errorf("--listen is required")
			}
			err := proxyfwd.RunGuest(cmd.Context(), listen, stdioRWC{in: os.Stdin, out: os.Stdout}, proxyfwd.GuestOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "proxy-guest: %v\n", err)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "", "guest loopback listen address (e.g. 127.0.0.1:8888)")
	return cmd
}
