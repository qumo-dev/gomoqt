package quicgo

import (
	"context"
	"time"

	"github.com/okdaichi/gomoqt/transport"
	quicgo_quicgo "github.com/quic-go/quic-go"
)

var _ transport.Stream = (*rawQuicStream)(nil)

type rawQuicStream struct {
	stream *quicgo_quicgo.Stream
}

func (wrapper rawQuicStream) Read(b []byte) (int, error) {
	return wrapper.stream.Read(b)
}

func (wrapper rawQuicStream) Write(b []byte) (int, error) {
	return wrapper.stream.Write(b)
}

func (wrapper rawQuicStream) CancelRead(code transport.StreamErrorCode) {
	wrapper.stream.CancelRead(quicgo_quicgo.StreamErrorCode(code))
}

func (wrapper rawQuicStream) CancelWrite(code transport.StreamErrorCode) {
	wrapper.stream.CancelWrite(quicgo_quicgo.StreamErrorCode(code))
}

func (wrapper rawQuicStream) SetDeadline(time time.Time) error {
	return wrapper.stream.SetDeadline(time)
}

func (wrapper rawQuicStream) SetReadDeadline(time time.Time) error {
	return wrapper.stream.SetReadDeadline(time)
}

func (wrapper rawQuicStream) SetWriteDeadline(time time.Time) error {
	return wrapper.stream.SetWriteDeadline(time)
}

func (wrapper rawQuicStream) Close() error {
	return wrapper.stream.Close()
}

func (wrapper rawQuicStream) Context() context.Context {
	return wrapper.stream.Context()
}

/*
 *
 */
var _ transport.ReceiveStream = (*rawQuicReceiveStream)(nil)

type rawQuicReceiveStream struct {
	stream *quicgo_quicgo.ReceiveStream
}

func (wrapper rawQuicReceiveStream) Read(b []byte) (int, error) {
	return wrapper.stream.Read(b)
}

func (wrapper rawQuicReceiveStream) CancelRead(code transport.StreamErrorCode) {
	wrapper.stream.CancelRead(quicgo_quicgo.StreamErrorCode(code))
}

func (wrapper rawQuicReceiveStream) SetReadDeadline(time time.Time) error {
	return wrapper.stream.SetReadDeadline(time)
}

/*
 *
 */

var _ transport.SendStream = (*rawQuicSendStream)(nil)

type rawQuicSendStream struct {
	stream *quicgo_quicgo.SendStream
}

func (wrapper rawQuicSendStream) Write(b []byte) (int, error) {
	return wrapper.stream.Write(b)
}

func (wrapper rawQuicSendStream) CancelWrite(code transport.StreamErrorCode) {
	wrapper.stream.CancelWrite(quicgo_quicgo.StreamErrorCode(code))
}

func (wrapper rawQuicSendStream) SetWriteDeadline(time time.Time) error {
	return wrapper.stream.SetWriteDeadline(time)
}

func (wrapper rawQuicSendStream) Close() error {
	return wrapper.stream.Close()
}

func (wrapper rawQuicSendStream) Context() context.Context {
	return wrapper.stream.Context()
}
