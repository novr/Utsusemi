package main

import (
	"fmt"
	"net"
	"os"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/actioncache"
)

func newCacheServeCmd() *cobra.Command {
	var (
		addr string
		dir  string
	)
	cmd := &cobra.Command{
		Use:    "cache-serve",
		Short:  "Serve local Actions cache API (guest bootstrap)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				return fmt.Errorf("--dir is required")
			}
			if addr == "" {
				addr = actioncache.DefaultAddr
			}
			if err := requireLoopbackAddr(addr); err != nil {
				return err
			}
			store, err := actioncache.NewStore(dir)
			if err != nil {
				return err
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "cache-serve listening on %s dir=%s\n", ln.Addr().String(), dir)
			return actioncache.ListenAndServe(ln, store)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", actioncache.DefaultAddr, "loopback listen address")
	cmd.Flags().StringVar(&dir, "dir", "", "cache store directory")
	return cmd
}

func requireLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("cache-serve --addr: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("cache-serve --addr must be loopback (got %q)", host)
	}
	return nil
}
