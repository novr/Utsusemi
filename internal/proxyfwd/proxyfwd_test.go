package proxyfwd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type duplexPipe struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (d *duplexPipe) Read(p []byte) (int, error)  { return d.r.Read(p) }
func (d *duplexPipe) Write(p []byte) (int, error) { return d.w.Write(p) }
func (d *duplexPipe) Close() error {
	_ = d.r.Close()
	_ = d.w.Close()
	return nil
}

func newPipePair() (hostRW, guestRW io.ReadWriteCloser) {
	h2gR, h2gW := io.Pipe()
	g2hR, g2hW := io.Pipe()
	return &duplexPipe{r: g2hR, w: h2gW}, &duplexPipe{r: h2gR, w: g2hW}
}

type fakeResolver struct {
	mu    sync.Mutex
	addrs map[string][]netip.Addr
	err   error
	calls int32
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]netip.Addr, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return append([]netip.Addr(nil), f.addrs[host]...), nil
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func startEchoUpstream(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func startTestTunnel(t *testing.T, listen, upstream string, res *fakeResolver, cfgFn func(*Forwarder)) (func(), *Forwarder) {
	t.Helper()
	hostRW, guestRW := newPipePair()
	guestCtx, guestCancel := context.WithCancel(context.Background())
	guestDone := make(chan error, 1)
	go func() {
		guestDone <- RunGuest(guestCtx, listen, guestRW, GuestOptions{})
	}()

	fwd := New(listen, upstream, res, nil)
	if cfgFn != nil {
		cfgFn(fwd)
	}
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readyCancel()
	tunnel, err := fwd.ServePipe(readyCtx, hostRW)
	if err != nil {
		guestCancel()
		<-guestDone
		t.Fatalf("ServePipe: %v", err)
	}
	return func() {
		_ = tunnel.Close()
		guestCancel()
		<-guestDone
	}, fwd
}

func TestTunnelEchoAndLargeTransfer(t *testing.T) {
	upAddr, closeUp := startEchoUpstream(t)
	defer closeUp()
	listen := freeLoopbackAddr(t)

	cleanup, _ := startTestTunnel(t, listen, upAddr, &fakeResolver{}, nil)
	defer cleanup()

	c, err := net.DialTimeout("tcp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// 2 MiB exercises multiple window updates (initial window is 256 KiB).
	payload := make([]byte, 2*1024*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	writeErr := make(chan error, 1)
	go func() {
		_, err := c.Write(payload)
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		writeErr <- err
	}()
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %d bytes want %d", len(got), len(payload))
	}
}

func TestTunnelConcurrentStreams(t *testing.T) {
	upAddr, closeUp := startEchoUpstream(t)
	defer closeUp()
	listen := freeLoopbackAddr(t)

	cleanup, _ := startTestTunnel(t, listen, upAddr, &fakeResolver{}, nil)
	defer cleanup()

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, err := net.DialTimeout("tcp", listen, 2*time.Second)
			if err != nil {
				errs <- err
				return
			}
			defer c.Close()
			msg := bytes.Repeat([]byte{byte('a' + idx)}, 64*1024)
			go func() {
				_, _ = c.Write(msg)
				if tc, ok := c.(*net.TCPConn); ok {
					_ = tc.CloseWrite()
				}
			}()
			got, err := io.ReadAll(c)
			if err != nil {
				errs <- err
				return
			}
			if !bytes.Equal(got, msg) {
				errs <- io.ErrUnexpectedEOF
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestTunnelHalfClosePreservesReverseDirection(t *testing.T) {
	upLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upLn.Close()
	go func() {
		c, err := upLn.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		req, _ := io.ReadAll(c)
		time.Sleep(30 * time.Millisecond)
		_, _ = c.Write([]byte("reply:" + string(req)))
	}()

	listen := freeLoopbackAddr(t)
	cleanup, _ := startTestTunnel(t, listen, upLn.Addr().String(), &fakeResolver{}, nil)
	defer cleanup()

	raw, err := net.DialTimeout("tcp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	tc := raw.(*net.TCPConn)
	if _, err := tc.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := tc.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(tc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "reply:hello" {
		t.Fatalf("got %q want %q", got, "reply:hello")
	}
}

func TestTunnelMaxConns(t *testing.T) {
	upLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upLn.Close()
	block := make(chan struct{})
	go func() {
		for {
			c, err := upLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				<-block
			}(c)
		}
	}()

	listen := freeLoopbackAddr(t)
	cleanup, _ := startTestTunnel(t, listen, upLn.Addr().String(), &fakeResolver{}, func(f *Forwarder) {
		f.MaxConns = 1
	})
	defer func() {
		close(block)
		cleanup()
	}()

	c1, err := net.DialTimeout("tcp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	time.Sleep(50 * time.Millisecond)

	c2, err := net.DialTimeout("tcp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	_ = c2.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	if _, err := c2.Read(buf); err == nil {
		t.Fatal("expected second connection to be rejected by max conns")
	}
}

func TestTunnelResolveCacheAndInvalidate(t *testing.T) {
	upAddr, closeUp := startEchoUpstream(t)
	defer closeUp()
	host, port, _ := net.SplitHostPort(upAddr)
	ip := netip.MustParseAddr(host)
	res := &fakeResolver{addrs: map[string][]netip.Addr{"proxy.test": {ip}}}

	listen := freeLoopbackAddr(t)
	cleanup, _ := startTestTunnel(t, listen, net.JoinHostPort("proxy.test", port), res, func(f *Forwarder) {
		f.ResolveTTL = time.Hour
	})
	defer cleanup()

	for i := 0; i < 3; i++ {
		c, err := net.DialTimeout("tcp", listen, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = c.Write([]byte("x"))
		buf := make([]byte, 1)
		if _, err := io.ReadFull(c, buf); err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
	}
	if got := atomic.LoadInt32(&res.calls); got != 1 {
		t.Fatalf("resolver calls=%d want 1", got)
	}
}

func writeRawFrame(t *testing.T, w io.Writer, typ byte, id uint32, payload []byte) {
	t.Helper()
	hdr := make([]byte, frameHeaderSize)
	hdr[0] = typ
	binary.BigEndian.PutUint32(hdr[1:5], id)
	binary.BigEndian.PutUint32(hdr[5:9], uint32(len(payload)))
	if _, err := w.Write(append(hdr, payload...)); err != nil {
		t.Fatal(err)
	}
}

func readRawFrame(t *testing.T, r io.Reader) (typ byte, id uint32, payload []byte) {
	t.Helper()
	hdr := make([]byte, frameHeaderSize)
	if _, err := io.ReadFull(r, hdr); err != nil {
		t.Fatalf("read frame header: %v", err)
	}
	n := binary.BigEndian.Uint32(hdr[5:9])
	payload = make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatalf("read frame payload: %v", err)
	}
	return hdr[0], binary.BigEndian.Uint32(hdr[1:5]), payload
}

// rawGuest attaches a Forwarder to an in-memory pipe whose guest side is driven
// by the test with hand-written frames.
func rawGuest(t *testing.T, upstream string) (guest io.ReadWriteCloser, tunnel io.Closer) {
	t.Helper()
	hostRW, guestRW := newPipePair()
	fwd := New("127.0.0.1:8888", upstream, &fakeResolver{}, nil)
	fwd.ReadyTimeout = 2 * time.Second

	type result struct {
		c   io.Closer
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := fwd.ServePipe(context.Background(), hostRW)
		done <- result{c, err}
	}()
	writeRawFrame(t, guestRW, frameReady, 0, []byte("127.0.0.1:8888"))
	res := <-done
	if res.err != nil {
		t.Fatal(res.err)
	}
	t.Cleanup(func() {
		_ = res.c.Close()
		_ = guestRW.Close()
	})
	return guestRW, res.c
}

func TestHostRejectsDuplicateAndZeroStreamID(t *testing.T) {
	block := make(chan struct{})
	upLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upLn.Close()
	defer close(block)
	go func() {
		for {
			c, err := upLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				<-block
			}(c)
		}
	}()

	guest, _ := rawGuest(t, upLn.Addr().String())

	writeRawFrame(t, guest, frameOpen, 1, nil)
	if typ, id, _ := readRawFrame(t, guest); typ != frameOpenOK || id != 1 {
		t.Fatalf("first open: typ=%d id=%d", typ, id)
	}

	writeRawFrame(t, guest, frameOpen, 1, nil)
	if typ, id, p := readRawFrame(t, guest); typ != frameReset || id != 1 || string(p) != "duplicate stream" {
		t.Fatalf("duplicate open: typ=%d id=%d payload=%q", typ, id, p)
	}

	writeRawFrame(t, guest, frameOpen, 0, nil)
	if typ, id, _ := readRawFrame(t, guest); typ != frameReset || id != 0 {
		t.Fatalf("zero id open: typ=%d id=%d", typ, id)
	}
}

func TestHostClosesSessionOnProtocolViolation(t *testing.T) {
	for name, hdr := range map[string][]byte{
		"oversized frame": {frameData, 0, 0, 0, 1, 0xff, 0xff, 0xff, 0xff},
		"unknown type":    {99, 0, 0, 0, 1, 0, 0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			guest, tunnel := rawGuest(t, "127.0.0.1:1")
			if _, err := guest.Write(hdr); err != nil {
				t.Fatal(err)
			}
			sess := tunnel.(*hostTunnel).sess
			select {
			case <-sess.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("session should end on protocol violation")
			}
			if sess.err() == nil {
				t.Fatal("expected a recorded error")
			}
		})
	}
}

func TestStreamWindowAccounting(t *testing.T) {
	st := newMuxStream(&muxSession{streams: map[uint32]*muxStream{}}, 1)
	if !st.pushData(make([]byte, initialStreamWindow)) {
		t.Fatal("a full window must be accepted")
	}
	if st.pushData([]byte{1}) {
		t.Fatal("data beyond the receive window must be rejected")
	}

	st.addSendWindow(1 << 30)
	st.mu.Lock()
	win := st.sendWin
	st.mu.Unlock()
	if win > initialStreamWindow {
		t.Fatalf("send window %d exceeds cap %d", win, initialStreamWindow)
	}
}

func TestTunnelUpstreamUnreachableClosesClient(t *testing.T) {
	listen := freeLoopbackAddr(t)
	cleanup, _ := startTestTunnel(t, listen, freeLoopbackAddr(t), &fakeResolver{}, nil)
	defer cleanup()

	c, err := net.DialTimeout("tcp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected the client connection to be closed")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("client connection stayed open after upstream dial failure")
	}
}

func TestTunnelCloseWithActiveConnections(t *testing.T) {
	upLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upLn.Close()
	block := make(chan struct{})
	defer close(block)
	go func() {
		for {
			c, err := upLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				<-block
			}(c)
		}
	}()

	listen := freeLoopbackAddr(t)
	hostRW, guestRW := newPipePair()
	guestCtx, guestCancel := context.WithCancel(context.Background())
	defer guestCancel()
	guestDone := make(chan error, 1)
	go func() { guestDone <- RunGuest(guestCtx, listen, guestRW, GuestOptions{}) }()

	fwd := New(listen, upLn.Addr().String(), &fakeResolver{}, nil)
	tunnel, err := fwd.ServePipe(context.Background(), hostRW)
	if err != nil {
		t.Fatal(err)
	}

	var conns []net.Conn
	for i := 0; i < 4; i++ {
		c, err := net.DialTimeout("tcp", listen, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_, _ = c.Write(bytes.Repeat([]byte("x"), 1<<20))
		conns = append(conns, c)
	}
	time.Sleep(100 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		_ = tunnel.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("tunnel.Close hung with active connections")
	}
	select {
	case <-guestDone:
	case <-time.After(5 * time.Second):
		t.Fatal("RunGuest did not exit after the host closed the tunnel")
	}
	for _, c := range conns {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := c.Read(make([]byte, 1)); err == nil {
			t.Fatal("client connection should be closed")
		}
	}
}

func TestMaxConnsIsPerTunnel(t *testing.T) {
	upLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upLn.Close()
	block := make(chan struct{})
	defer close(block)
	go func() {
		for {
			c, err := upLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				<-block
			}(c)
		}
	}()

	fwd := New("127.0.0.1:8888", upLn.Addr().String(), &fakeResolver{}, nil)
	fwd.MaxConns = 1

	open := func() (io.ReadWriteCloser, io.Closer) {
		hostRW, guestRW := newPipePair()
		done := make(chan io.Closer, 1)
		go func() {
			c, err := fwd.ServePipe(context.Background(), hostRW)
			if err != nil {
				t.Error(err)
			}
			done <- c
		}()
		writeRawFrame(t, guestRW, frameReady, 0, nil)
		return guestRW, <-done
	}
	g1, t1 := open()
	g2, t2 := open()
	defer t1.Close()
	defer t2.Close()

	for i, g := range []io.ReadWriteCloser{g1, g2} {
		writeRawFrame(t, g, frameOpen, 1, nil)
		if typ, _, _ := readRawFrame(t, g); typ != frameOpenOK {
			t.Fatalf("tunnel %d: first stream rejected (typ=%d); limit must not be shared", i, typ)
		}
	}
	writeRawFrame(t, g1, frameOpen, 2, nil)
	if typ, id, _ := readRawFrame(t, g1); typ != frameReset || id != 2 {
		t.Fatalf("second stream on a full tunnel: typ=%d id=%d", typ, id)
	}
}

func TestRunGuestRejectsNonLoopback(t *testing.T) {
	_, guestRW := newPipePair()
	err := RunGuest(context.Background(), "192.168.64.1:8888", guestRW, GuestOptions{})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err=%v want loopback error", err)
	}
}

func TestStageBinAndWithMount(t *testing.T) {
	stateDir := t.TempDir()
	if err := StageBin(stateDir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(BinDir(stateDir), BinaryName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected executable mode, got %v", info.Mode())
	}

	mounts := WithMount(stateDir, false, []string{"a:/tmp/a:ro"})
	if len(mounts) != 1 {
		t.Fatalf("disabled should not add mount: %v", mounts)
	}
	mounts = WithMount(stateDir, true, []string{"a:/tmp/a:ro"})
	absDir, err := filepath.Abs(BinDir(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	want := MountName + ":" + absDir + ":ro"
	if len(mounts) != 2 || mounts[1] != want {
		t.Fatalf("mounts=%v want %q", mounts, want)
	}
	// relative stateDir resolves to an absolute host path for provider.HostPathFromDir
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(cwd, stateDir); err == nil && !filepath.IsAbs(rel) {
		relMounts := WithMount(rel, true, nil)
		if len(relMounts) != 1 || relMounts[0] != want {
			t.Fatalf("expected absolute host path %q, got %v", want, relMounts)
		}
	}
	// idempotent
	mounts = WithMount(stateDir, true, mounts)
	if len(mounts) != 2 {
		t.Fatalf("duplicate mount added: %v", mounts)
	}
}
