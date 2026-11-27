package proxyfwd

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	frameReady     byte = 1
	frameOpen      byte = 2
	frameOpenOK    byte = 3
	frameData      byte = 4
	frameHalfClose byte = 5
	frameReset     byte = 6
	frameWindow    byte = 7

	frameHeaderSize       = 9
	maxFramePayload       = 32 * 1024
	initialStreamWindow   = 256 * 1024
	windowUpdateThreshold = 128 * 1024
)

var errSessionClosed = errors.New("proxyfwd: tunnel closed")

type muxSession struct {
	rw     io.ReadWriteCloser
	onOpen func(*muxStream)

	writeMu sync.Mutex

	mu      sync.Mutex
	streams map[uint32]*muxStream
	nextID  uint32
	closed  bool

	readyOnce sync.Once
	readyCh   chan string

	doneOnce  sync.Once
	done      chan struct{}
	readErr   error
	handlerWG sync.WaitGroup
}

func newMuxSession(rw io.ReadWriteCloser, onOpen func(*muxStream)) *muxSession {
	s := &muxSession{
		rw:      rw,
		onOpen:  onOpen,
		streams: make(map[uint32]*muxStream),
		readyCh: make(chan string, 1),
		done:    make(chan struct{}),
	}
	go s.readLoop()
	return s
}

func (s *muxSession) Done() <-chan struct{} { return s.done }

// err returns the read-loop failure that ended the session (valid after Done).
func (s *muxSession) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return s.readErr
	}
	return errSessionClosed
}

func (s *muxSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	streams := make([]*muxStream, 0, len(s.streams))
	for _, st := range s.streams {
		streams = append(streams, st)
	}
	s.streams = make(map[uint32]*muxStream)
	s.mu.Unlock()

	for _, st := range streams {
		st.abort(errSessionClosed)
	}
	return s.rw.Close()
}

// Wait blocks until readLoop and all in-flight stream handlers have exited.
func (s *muxSession) Wait() {
	<-s.done
	s.handlerWG.Wait()
}

func (s *muxSession) sendReady(addr string) error {
	return s.writeFrame(frameReady, 0, []byte(addr))
}

func (s *muxSession) waitReady(ctx context.Context) (string, error) {
	select {
	case addr := <-s.readyCh:
		return addr, nil
	case <-s.done:
		s.mu.Lock()
		err := s.readErr
		s.mu.Unlock()
		if err != nil {
			return "", err
		}
		return "", errSessionClosed
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *muxSession) openStream(ctx context.Context) (*muxStream, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errSessionClosed
	}
	s.nextID++
	id := s.nextID
	st := newMuxStream(s, id)
	s.streams[id] = st
	s.mu.Unlock()

	if err := s.writeFrame(frameOpen, id, nil); err != nil {
		s.removeStream(id)
		st.abort(err)
		return nil, err
	}

	select {
	case err := <-st.openCh:
		if err != nil {
			s.removeStream(id)
			return nil, err
		}
		return st, nil
	case <-s.done:
		s.removeStream(id)
		return nil, errSessionClosed
	case <-ctx.Done():
		s.removeStream(id)
		_ = st.Close()
		return nil, ctx.Err()
	}
}

func (s *muxSession) writeFrame(typ byte, id uint32, payload []byte) error {
	if len(payload) > maxFramePayload {
		return fmt.Errorf("proxyfwd: frame payload too large (%d)", len(payload))
	}
	var hdr [frameHeaderSize]byte
	hdr[0] = typ
	binary.BigEndian.PutUint32(hdr[1:5], id)
	binary.BigEndian.PutUint32(hdr[5:9], uint32(len(payload)))

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := writeAll(s.rw, hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if err := writeAll(s.rw, payload); err != nil {
			return err
		}
	}
	return nil
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

func (s *muxSession) readLoop() {
	defer func() {
		s.mu.Lock()
		s.closed = true
		streams := make([]*muxStream, 0, len(s.streams))
		for _, st := range s.streams {
			streams = append(streams, st)
		}
		s.streams = make(map[uint32]*muxStream)
		s.mu.Unlock()
		for _, st := range streams {
			st.abort(errSessionClosed)
		}
		s.doneOnce.Do(func() { close(s.done) })
	}()

	setErr := func(err error) {
		s.mu.Lock()
		if s.readErr == nil {
			s.readErr = err
		}
		s.mu.Unlock()
	}

	var hdr [frameHeaderSize]byte
	for {
		if _, err := io.ReadFull(s.rw, hdr[:]); err != nil {
			setErr(err)
			return
		}
		typ := hdr[0]
		id := binary.BigEndian.Uint32(hdr[1:5])
		length := binary.BigEndian.Uint32(hdr[5:9])
		if length > maxFramePayload {
			setErr(fmt.Errorf("proxyfwd: invalid frame length %d", length))
			return
		}
		var payload []byte
		if length > 0 {
			payload = make([]byte, length)
			if _, err := io.ReadFull(s.rw, payload); err != nil {
				setErr(err)
				return
			}
		}

		switch typ {
		case frameReady:
			s.readyOnce.Do(func() {
				s.readyCh <- string(payload)
			})
		case frameOpen:
			if s.onOpen == nil || id == 0 {
				go func(sid uint32) { _ = s.writeFrame(frameReset, sid, []byte("unsupported")) }(id)
				continue
			}
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				return
			}
			if _, dup := s.streams[id]; dup {
				s.mu.Unlock()
				go func(sid uint32) { _ = s.writeFrame(frameReset, sid, []byte("duplicate stream")) }(id)
				continue
			}
			st := newMuxStream(s, id)
			s.streams[id] = st
			s.mu.Unlock()
			s.handlerWG.Add(1)
			go func(st *muxStream) {
				defer s.handlerWG.Done()
				s.onOpen(st)
			}(st)
		case frameOpenOK:
			if st := s.getStream(id); st != nil {
				st.notifyOpen(nil)
			}
		case frameData:
			if st := s.getStream(id); st != nil {
				if !st.pushData(payload) {
					s.removeStream(id)
					st.abort(errors.New("proxyfwd: receive window exceeded"))
					go func(sid uint32) { _ = s.writeFrame(frameReset, sid, []byte("window exceeded")) }(id)
				}
			}
		case frameHalfClose:
			if st := s.getStream(id); st != nil {
				st.onRemoteEOF()
			}
		case frameReset:
			if st := s.getStream(id); st != nil {
				s.removeStream(id)
				msg := string(payload)
				if msg == "" {
					msg = "stream reset"
				}
				st.abort(errors.New(msg))
			}
		case frameWindow:
			if len(payload) == 4 {
				delta := binary.BigEndian.Uint32(payload)
				if st := s.getStream(id); st != nil {
					st.addSendWindow(int64(delta))
				}
			}
		default:
			setErr(fmt.Errorf("proxyfwd: unknown frame type %d", typ))
			return
		}
	}
}

func (s *muxSession) getStream(id uint32) *muxStream {
	s.mu.Lock()
	st := s.streams[id]
	s.mu.Unlock()
	return st
}

func (s *muxSession) removeStream(id uint32) {
	s.mu.Lock()
	delete(s.streams, id)
	s.mu.Unlock()
}

type muxStream struct {
	sess *muxSession
	id   uint32

	openOnce sync.Once
	openCh   chan error

	mu           sync.Mutex
	sendWin      int64
	winCh        chan struct{}
	recvBuf      [][]byte
	recvQueued   int
	recvConsumed uint32
	recvCh       chan struct{}
	remoteEOF    bool
	localEOF     bool
	reset        bool
	resetErr     error
	closeOnce    sync.Once
	closed       chan struct{}
}

func newMuxStream(sess *muxSession, id uint32) *muxStream {
	return &muxStream{
		sess:    sess,
		id:      id,
		openCh:  make(chan error, 1),
		sendWin: initialStreamWindow,
		winCh:   make(chan struct{}, 1),
		recvCh:  make(chan struct{}, 1),
		closed:  make(chan struct{}),
	}
}

func (st *muxStream) notifyOpen(err error) {
	st.openOnce.Do(func() {
		st.openCh <- err
	})
}

func (st *muxStream) accept() error {
	return st.sess.writeFrame(frameOpenOK, st.id, nil)
}

func (st *muxStream) reject(reason string) {
	st.sess.removeStream(st.id)
	st.abort(errors.New(reason))
	_ = st.sess.writeFrame(frameReset, st.id, []byte(reason))
}

func (st *muxStream) pushData(data []byte) bool {
	st.mu.Lock()
	if st.reset || st.remoteEOF {
		st.mu.Unlock()
		return true
	}
	if st.recvQueued+len(data) > initialStreamWindow {
		st.mu.Unlock()
		return false
	}
	st.recvBuf = append(st.recvBuf, data)
	st.recvQueued += len(data)
	st.mu.Unlock()
	notifyChan(st.recvCh)
	return true
}

func (st *muxStream) onRemoteEOF() {
	st.mu.Lock()
	st.remoteEOF = true
	bothDone := st.localEOF && st.remoteEOF
	st.mu.Unlock()
	if bothDone {
		st.sess.removeStream(st.id)
	}
	notifyChan(st.recvCh)
}

func (st *muxStream) addSendWindow(delta int64) {
	st.mu.Lock()
	st.sendWin += delta
	if st.sendWin > initialStreamWindow {
		st.sendWin = initialStreamWindow
	}
	st.mu.Unlock()
	notifyChan(st.winCh)
}

func (st *muxStream) abort(err error) {
	st.mu.Lock()
	if !st.reset {
		st.reset = true
		st.resetErr = err
	}
	st.mu.Unlock()
	st.notifyOpen(err)
	st.closeOnce.Do(func() { close(st.closed) })
	notifyChan(st.recvCh)
	notifyChan(st.winCh)
}

func (st *muxStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		st.mu.Lock()
		if len(st.recvBuf) > 0 {
			head := st.recvBuf[0]
			n := copy(p, head)
			if n == len(head) {
				st.recvBuf[0] = nil
				st.recvBuf = st.recvBuf[1:]
			} else {
				st.recvBuf[0] = head[n:]
			}
			st.recvQueued -= n
			st.recvConsumed += uint32(n)
			var sendDelta uint32
			if !st.remoteEOF && !st.reset && st.recvConsumed >= windowUpdateThreshold {
				sendDelta = st.recvConsumed
				st.recvConsumed = 0
			}
			st.mu.Unlock()
			if sendDelta > 0 {
				var buf [4]byte
				binary.BigEndian.PutUint32(buf[:], sendDelta)
				_ = st.sess.writeFrame(frameWindow, st.id, buf[:])
			}
			return n, nil
		}
		if st.remoteEOF {
			st.mu.Unlock()
			return 0, io.EOF
		}
		if st.reset {
			err := st.resetErr
			st.mu.Unlock()
			if err == nil {
				err = io.ErrClosedPipe
			}
			return 0, err
		}
		st.mu.Unlock()

		select {
		case <-st.recvCh:
		case <-st.closed:
		case <-st.sess.done:
			return 0, io.EOF
		}
	}
}

func (st *muxStream) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		chunk, err := st.reserveSend(len(p))
		if err != nil {
			return written, err
		}
		if err := st.sess.writeFrame(frameData, st.id, p[:chunk]); err != nil {
			return written, err
		}
		p = p[chunk:]
		written += chunk
	}
	return written, nil
}

func (st *muxStream) reserveSend(want int) (int, error) {
	for {
		st.mu.Lock()
		if st.reset {
			err := st.resetErr
			st.mu.Unlock()
			if err == nil {
				err = io.ErrClosedPipe
			}
			return 0, err
		}
		if st.localEOF {
			st.mu.Unlock()
			return 0, io.ErrClosedPipe
		}
		if st.sendWin > 0 {
			n := want
			if n > maxFramePayload {
				n = maxFramePayload
			}
			if int64(n) > st.sendWin {
				n = int(st.sendWin)
			}
			st.sendWin -= int64(n)
			st.mu.Unlock()
			return n, nil
		}
		st.mu.Unlock()

		select {
		case <-st.winCh:
		case <-st.closed:
		case <-st.sess.done:
			return 0, errSessionClosed
		}
	}
}

func (st *muxStream) CloseWrite() error {
	st.mu.Lock()
	if st.localEOF || st.reset {
		st.mu.Unlock()
		return nil
	}
	st.localEOF = true
	bothDone := st.localEOF && st.remoteEOF
	st.mu.Unlock()
	if bothDone {
		st.sess.removeStream(st.id)
	}
	return st.sess.writeFrame(frameHalfClose, st.id, nil)
}

func (st *muxStream) Close() error {
	st.mu.Lock()
	needReset := !st.reset && !(st.localEOF && st.remoteEOF)
	st.reset = true
	if st.resetErr == nil {
		st.resetErr = io.ErrClosedPipe
	}
	st.mu.Unlock()

	st.sess.removeStream(st.id)
	st.closeOnce.Do(func() { close(st.closed) })
	notifyChan(st.recvCh)
	notifyChan(st.winCh)
	if needReset {
		return st.sess.writeFrame(frameReset, st.id, nil)
	}
	return nil
}

func notifyChan(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
