package webtransportgo

import (
	"context"
	"sync"
	"time"

	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
	"github.com/qumo-dev/gomoqt/transport"
)

var _ transport.Stream = (*streamWrapper)(nil)

// streamWrapper reports the stream's errors as native QUIC would: see
// streamError and streamCause.
type streamWrapper struct {
	stream *quicgo_webtransportgo.Stream

	ctxOnce sync.Once
	ctx     context.Context
}

func (wrapper *streamWrapper) Read(b []byte) (int, error) {
	n, err := wrapper.stream.Read(b)
	return n, streamError(wrapper.stream.StreamID(), err)
}

func (wrapper *streamWrapper) Write(b []byte) (int, error) {
	n, err := wrapper.stream.Write(b)
	return n, streamError(wrapper.stream.StreamID(), err)
}

func (wrapper *streamWrapper) CancelRead(code transport.StreamErrorCode) {
	wrapper.stream.CancelRead(quicgo_webtransportgo.StreamErrorCode(code))
}

func (wrapper *streamWrapper) CancelWrite(code transport.StreamErrorCode) {
	wrapper.stream.CancelWrite(quicgo_webtransportgo.StreamErrorCode(code))
}

func (wrapper *streamWrapper) SetDeadline(time time.Time) error {
	return wrapper.stream.SetDeadline(time)
}

func (wrapper *streamWrapper) SetReadDeadline(time time.Time) error {
	return wrapper.stream.SetReadDeadline(time)
}

func (wrapper *streamWrapper) SetWriteDeadline(time time.Time) error {
	return wrapper.stream.SetWriteDeadline(time)
}

func (wrapper *streamWrapper) Close() error {
	return streamError(wrapper.stream.StreamID(), wrapper.stream.Close())
}

func (wrapper *streamWrapper) Context() context.Context {
	wrapper.ctxOnce.Do(func() { wrapper.ctx = streamContext(wrapper.stream.Context(), wrapper.stream.StreamID()) })
	return wrapper.ctx
}

func (wrapper *streamWrapper) SetPriority(urgency int8, incremental bool) {
	wrapper.stream.SetPriority(urgency, incremental)
}

var _ transport.ReceiveStream = (*receiveStreamWrapper)(nil)

type receiveStreamWrapper struct {
	stream *quicgo_webtransportgo.ReceiveStream
}

func (wrapper *receiveStreamWrapper) Read(b []byte) (int, error) {
	n, err := wrapper.stream.Read(b)
	return n, streamError(wrapper.stream.StreamID(), err)
}

func (wrapper *receiveStreamWrapper) CancelRead(code transport.StreamErrorCode) {
	wrapper.stream.CancelRead(quicgo_webtransportgo.StreamErrorCode(code))
}

func (wrapper *receiveStreamWrapper) SetReadDeadline(time time.Time) error {
	return wrapper.stream.SetReadDeadline(time)
}

var _ transport.SendStream = (*sendStreamWrapper)(nil)

type sendStreamWrapper struct {
	stream *quicgo_webtransportgo.SendStream

	ctxOnce sync.Once
	ctx     context.Context
}

func (wrapper *sendStreamWrapper) Write(b []byte) (int, error) {
	n, err := wrapper.stream.Write(b)
	return n, streamError(wrapper.stream.StreamID(), err)
}

func (wrapper *sendStreamWrapper) CancelWrite(code transport.StreamErrorCode) {
	wrapper.stream.CancelWrite(quicgo_webtransportgo.StreamErrorCode(code))
}

func (wrapper *sendStreamWrapper) SetWriteDeadline(time time.Time) error {
	return wrapper.stream.SetWriteDeadline(time)
}

func (wrapper *sendStreamWrapper) Close() error {
	return streamError(wrapper.stream.StreamID(), wrapper.stream.Close())
}

func (wrapper *sendStreamWrapper) Context() context.Context {
	wrapper.ctxOnce.Do(func() { wrapper.ctx = streamContext(wrapper.stream.Context(), wrapper.stream.StreamID()) })
	return wrapper.ctx
}

func (wrapper *sendStreamWrapper) SetPriority(urgency int8, incremental bool) {
	wrapper.stream.SetPriority(urgency, incremental)
}
