package proxyfwd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

// GuestOptions configures RunGuest (defaults match host defaults).
type GuestOptions struct {
	MaxConns     int
	KeepAlive    time.Duration
	DrainTimeout time.Duration
	OpenTimeout  time.Duration
}

// RunGuest listens on a guest loopback address and tunnels accepted TCP
// connections to the host over rw (stdin/stdout of `tart exec -i`).
func RunGuest(ctx context.Context, listen string, rw io.ReadWriteCloser, opts GuestOptions) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("proxy-guest listen: %w", err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		return fmt.Errorf("proxy-guest listen must be a loopback IP:port (got %q)", listen)
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(ip.Unmap().String(), port))
	if err != nil {
		return fmt.Errorf("proxy-guest listen %s: %w", listen, err)
	}
	defer ln.Close()

	sess := newMuxSession(rw, nil)
	defer sess.Close()

	if err := sess.sendReady(ln.Addr().String()); err != nil {
		return fmt.Errorf("proxy-guest ready: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-sess.Done():
		case <-runCtx.Done():
		}
		cancel()
		_ = ln.Close()
	}()

	maxConns := opts.MaxConns
	if maxConns <= 0 {
		maxConns = DefaultMaxConns
	}
	openTimeout := opts.OpenTimeout
	if openTimeout <= 0 {
		openTimeout = DefaultDialTimeout + 5*time.Second
	}

	var active int32
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if runCtx.Err() != nil {
				return nil
			}
			select {
			case <-sess.Done():
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		if int(atomic.LoadInt32(&active)) >= maxConns {
			_ = conn.Close()
			continue
		}
		atomic.AddInt32(&active, 1)
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			defer atomic.AddInt32(&active, -1)
			defer c.Close()
			setKeepAlive(c, opts.KeepAlive)

			openCtx, openCancel := context.WithTimeout(runCtx, openTimeout)
			stream, err := sess.openStream(openCtx)
			openCancel()
			if err != nil {
				return
			}
			defer stream.Close()
			relayBiDir(runCtx, c, stream, opts.DrainTimeout)
		}(conn)
	}
}
