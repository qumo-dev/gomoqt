package moqt

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
)

// streamResult is one queued I/O outcome for a fake stream.
// On Read, Data is copied into the caller's buffer; on Write it is ignored.
// Block parks the call until the stream is cancelled or closed, modelling a
// peer that has gone quiet without hanging up — reach for it instead of EOF
// when a reader goroutine must stay alive for the duration of the test.
type streamResult struct {
	Data  []byte
	Err   error
	Block bool
}

// resultQueue returns queued results in order, repeating the last entry once
// exhausted. The zero value is an empty queue, which callers treat as the
// direction's default (io.EOF for reads, success for writes).
type resultQueue struct {
	entries []streamResult
	idx     int
	off     int // offset within entries[idx], for partial reads
}

// next reports the entry for the current call, or ok=false for an empty queue.
func (q *resultQueue) next() (streamResult, bool) {
	if len(q.entries) == 0 {
		return streamResult{}, false
	}
	return q.entries[min(q.idx, len(q.entries)-1)], true
}

// readInto copies the current entry into p, advancing once it is consumed.
// An entry carrying both Data and Err yields its error only with the final
// chunk, so a caller buffer smaller than Data cannot truncate the data.
func (q *resultQueue) readInto(p []byte) (int, error) {
	r, ok := q.next()
	if !ok {
		return 0, io.EOF
	}
	n := copy(p, r.Data[min(q.off, len(r.Data)):])
	q.off += n
	if q.off < len(r.Data) {
		return n, nil
	}
	q.idx++
	q.off = 0
	return n, r.Err
}

// drained reports whether every queued entry has been consumed at least once.
func (q *resultQueue) drained() bool {
	return q.idx >= len(q.entries)
}

// blocking reports whether the current entry parks the caller.
func (q *resultQueue) blocking() bool {
	r, ok := q.next()
	return ok && r.Block
}

// advance consumes the current entry and returns its error (write direction).
func (q *resultQueue) advance() error {
	r, ok := q.next()
	if !ok {
		return nil
	}
	q.idx++
	return r.Err
}

// FakeQUICStream is a fake implementation of transport.Stream for testing.
// By default it models quic-go behavior:
//   - Context() is the send-side context (cancelled by Close / CancelWrite, NOT by Read errors)
//   - Close cancels Context() with nil cause (context.Cause returns context.Canceled)
//   - CancelWrite cancels Context() with *transport.StreamError cause
//   - CancelRead makes subsequent Read calls return *transport.StreamError (does NOT cancel Context)
//
// Read and Write behavior is driven by the Reads/Writes results queues: entries
// are returned in order and the last entry repeats once exhausted; an empty
// queue means io.EOF on Read and success on Write. Bytes passed to Write are
// recorded and readable via Written.
type FakeQUICStream struct {
	mu sync.Mutex

	Reads  []streamResult
	Writes []streamResult

	// ReadFrom/WriteTo back the stream with a real io source/sink, for cases a
	// finite queue cannot express — an endless generator, or a discard sink in
	// a benchmark. They apply only when the corresponding queue is empty; an
	// explicit queue wins over the sink. Bytes routed to WriteTo are NOT also
	// recorded for Written, so a benchmark sink does not grow an ever-larger
	// capture buffer; use Written or WriteTo, not both.
	ReadFrom io.Reader
	WriteTo  io.Writer

	ParentCtx context.Context // optional parent context; default: context.Background()

	// Error overrides; zero value means the call succeeds.
	CloseErr            error
	SetDeadlineErr      error
	SetReadDeadlineErr  error
	SetWriteDeadlineErr error

	// ReadGate, when set, also releases a read parked on a Block entry, so a
	// test can decide when the peer speaks again.
	ReadGate <-chan struct{}

	// WriteDelay stalls every Write, modelling a slow consumer. Under
	// testing/synctest this advances virtual time rather than real time.
	WriteDelay time.Duration

	// Notification channels, for tests that must observe a call as it happens.
	// Each send is non-blocking, so an unbuffered channel with no reader is safe.
	ReadNotify        chan<- struct{}
	WriteNotify       chan<- struct{}
	CancelReadNotify  chan<- transport.StreamErrorCode
	CancelWriteNotify chan<- transport.StreamErrorCode
	// DrainNotify is signalled once every queued read has been consumed, which
	// is what a test means by "the stream has been fully read" — ReadNotify
	// fires on every Read, including the first.
	DrainNotify chan<- struct{}

	reads  resultQueue
	writes resultQueue

	written          []byte
	writeCalls       int
	cancelReadCodes  []transport.StreamErrorCode
	cancelWriteCodes []transport.StreamErrorCode

	unblock chan struct{} // closed by CancelRead/Close to release Block reads

	ctx            context.Context
	cancelCause    context.CancelCauseFunc
	cancelReadErr  error
	closed         bool  // true after Close (finishedWriting in quic-go)
	cancelWriteErr error // non-nil after CancelWrite (resetErr in quic-go)

	prioritySet         bool
	priorityUrgency     int8
	priorityIncremental bool
}

var _ transport.Stream = (*FakeQUICStream)(nil)

// ensureContext lazily initialises the internal cancellable context.
// Must be called with f.mu held.
func (f *FakeQUICStream) ensureContext() {
	if f.ctx == nil {
		parent := f.ParentCtx
		if parent == nil {
			parent = context.Background()
		}
		f.ctx, f.cancelCause = context.WithCancelCause(parent)
	}
}

// syncQueues copies the configured entries into the internal queues once.
// Must be called with f.mu held.
func (f *FakeQUICStream) syncQueues() {
	if f.reads.entries == nil && f.Reads != nil {
		f.reads.entries = f.Reads
	}
	if f.writes.entries == nil && f.Writes != nil {
		f.writes.entries = f.Writes
	}
}

func (f *FakeQUICStream) Read(p []byte) (int, error) {
	f.mu.Lock()
	if f.cancelReadErr != nil {
		err := f.cancelReadErr
		f.mu.Unlock()
		return 0, err
	}
	f.syncQueues()
	if f.reads.blocking() {
		f.ensureUnblock()
		f.ensureContext()
		unblock := f.unblock
		ctx := f.ctx
		gate := f.ReadGate
		f.mu.Unlock()

		// Wake on an explicit cancel/close, on the stream context being
		// cancelled (how production tears these streams down), or on a gate
		// the test controls.
		select {
		case <-unblock:
		case <-ctx.Done():
		case <-gate:
		}

		f.mu.Lock()
		if f.cancelReadErr != nil {
			err := f.cancelReadErr
			f.mu.Unlock()
			return 0, err
		}
		// A Block entry with more behind it is a pause, not an ending: step
		// past it and serve what follows. A trailing Block ends the stream.
		if f.reads.idx < len(f.reads.entries)-1 {
			f.reads.idx++
			f.reads.off = 0
			n, err := f.readQueueLocked(p)
			f.mu.Unlock()
			signal(f.ReadNotify)
			return n, err
		}
		f.mu.Unlock()
		return 0, io.EOF
	}
	notify := f.ReadNotify
	if len(f.reads.entries) == 0 && f.ReadFrom != nil {
		src := f.ReadFrom
		f.mu.Unlock()
		n, err := src.Read(p)
		signal(notify)
		return n, err
	}
	n, err := f.readQueueLocked(p)
	f.mu.Unlock()
	signal(notify)
	return n, err
}

// readQueueLocked serves the queue and signals DrainNotify once it is
// exhausted. Must be called with f.mu held.
func (f *FakeQUICStream) readQueueLocked(p []byte) (int, error) {
	n, err := f.reads.readInto(p)
	if f.reads.drained() {
		signal(f.DrainNotify)
	}
	return n, err
}

// signal performs a non-blocking send, so an unread channel never stalls a fake.
func signal(ch chan<- struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// ensureUnblock lazily creates the channel that releases blocked reads.
// Must be called with f.mu held.
func (f *FakeQUICStream) ensureUnblock() {
	if f.unblock == nil {
		f.unblock = make(chan struct{})
	}
}

// releaseReads wakes any read parked on a Block entry.
// Must be called with f.mu held.
func (f *FakeQUICStream) releaseReads() {
	f.ensureUnblock()
	select {
	case <-f.unblock: // already closed
	default:
		close(f.unblock)
	}
}

func (f *FakeQUICStream) Write(p []byte) (int, error) {
	f.mu.Lock()
	f.syncQueues()
	f.writeCalls++
	delay := f.WriteDelay
	err := f.writes.advance()
	sink := f.WriteTo
	if len(f.writes.entries) > 0 {
		sink = nil // an explicit queue result wins over the sink
	}
	if err == nil && sink == nil {
		// A sink owns the bytes; recording them too would make every
		// discard-backed benchmark grow an unbounded capture buffer.
		f.written = append(f.written, p...)
	}
	notify := f.WriteNotify
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if notify != nil {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
	if err != nil {
		return 0, err
	}
	if sink != nil {
		return sink.Write(p)
	}
	return len(p), nil
}

// WriteCalls returns how many times Write has been called.
func (f *FakeQUICStream) WriteCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeCalls
}

// Written returns a copy of every byte passed to a successful Write.
func (f *FakeQUICStream) Written() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]byte, len(f.written))
	copy(out, f.written)
	return out
}

func (f *FakeQUICStream) Close() error {
	f.mu.Lock()
	if f.CloseErr != nil {
		err := f.CloseErr
		f.mu.Unlock()
		return err
	}
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	cancelled := f.cancelWriteErr != nil
	f.ensureContext()
	f.releaseReads()
	cancel := f.cancelCause
	f.mu.Unlock()
	if cancelled {
		return fmt.Errorf("close called for canceled stream")
	}
	cancel(nil)
	return nil
}

func (f *FakeQUICStream) CancelRead(code transport.StreamErrorCode) {
	f.mu.Lock()
	f.cancelReadCodes = append(f.cancelReadCodes, code)
	if f.cancelReadErr == nil {
		f.cancelReadErr = &transport.StreamError{ErrorCode: code}
	}
	f.releaseReads()
	notify := f.CancelReadNotify
	f.mu.Unlock()

	if notify != nil {
		select {
		case notify <- code:
		default:
		}
	}
}

func (f *FakeQUICStream) CancelWrite(code transport.StreamErrorCode) {
	f.mu.Lock()
	f.cancelWriteCodes = append(f.cancelWriteCodes, code)
	if f.closed || f.cancelWriteErr != nil {
		notify := f.CancelWriteNotify
		f.mu.Unlock()
		if notify != nil {
			select {
			case notify <- code:
			default:
			}
		}
		return
	}
	f.cancelWriteErr = &transport.StreamError{ErrorCode: code}
	f.ensureContext()
	cancel := f.cancelCause
	notify := f.CancelWriteNotify
	f.mu.Unlock()

	cancel(f.cancelWriteErr)
	if notify != nil {
		select {
		case notify <- code:
		default:
		}
	}
}

// CancelReadCodes returns the codes passed to CancelRead, in order.
func (f *FakeQUICStream) CancelReadCodes() []transport.StreamErrorCode {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]transport.StreamErrorCode, len(f.cancelReadCodes))
	copy(out, f.cancelReadCodes)
	return out
}

// CancelWriteCodes returns the codes passed to CancelWrite, in order.
func (f *FakeQUICStream) CancelWriteCodes() []transport.StreamErrorCode {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]transport.StreamErrorCode, len(f.cancelWriteCodes))
	copy(out, f.cancelWriteCodes)
	return out
}

func (f *FakeQUICStream) Context() context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureContext()
	return f.ctx
}

func (f *FakeQUICStream) SetDeadline(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.SetDeadlineErr
}

func (f *FakeQUICStream) SetReadDeadline(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.SetReadDeadlineErr
}

func (f *FakeQUICStream) SetWriteDeadline(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.SetWriteDeadlineErr
}

func (f *FakeQUICStream) SetPriority(urgency int8, incremental bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prioritySet = true
	f.priorityUrgency = urgency
	f.priorityIncremental = incremental
}

// LastPriority reports the urgency/incremental values from the most recent
// SetPriority call, and whether SetPriority was ever called.
func (f *FakeQUICStream) LastPriority() (urgency int8, incremental bool, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.priorityUrgency, f.priorityIncremental, f.prioritySet
}
