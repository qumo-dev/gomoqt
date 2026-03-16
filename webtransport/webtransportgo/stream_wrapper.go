package webtransportgo

import (
	"context"
	"time"

	"github.com/okdaichi/gomoqt/quic"
	quicgo_webtransportgo "github.com/okdaichi/webtransport-go"
)

var _ quic.Stream = (*streamWrapper)(nil)

type streamWrapper struct {
	stream *quicgo_webtransportgo.Stream
}

func (wrapper streamWrapper) Read(b []byte) (int, error) {
	return wrapper.stream.Read(b)
}

func (wrapper streamWrapper) Write(b []byte) (int, error) {
	return wrapper.stream.Write(b)
}

func (wrapper streamWrapper) CancelRead(code quic.StreamErrorCode) {
	wrapper.stream.CancelRead(quicgo_webtransportgo.StreamErrorCode(code))
}

func (wrapper streamWrapper) CancelWrite(code quic.StreamErrorCode) {
	wrapper.stream.CancelWrite(quicgo_webtransportgo.StreamErrorCode(code))
}

func (wrapper streamWrapper) SetDeadline(time time.Time) error {
	return wrapper.stream.SetDeadline(time)
}

func (wrapper streamWrapper) SetReadDeadline(time time.Time) error {
	return wrapper.stream.SetReadDeadline(time)
}

func (wrapper streamWrapper) SetWriteDeadline(time time.Time) error {
	return wrapper.stream.SetWriteDeadline(time)
}

func (wrapper streamWrapper) Close() error {
	return wrapper.stream.Close()
}

func (wrapper streamWrapper) Context() context.Context {
	return wrapper.stream.Context()
}

/*
 *
 */
type receiveStreamWrapper struct {
	stream *quicgo_webtransportgo.ReceiveStream
}

func (wrapper receiveStreamWrapper) Read(b []byte) (int, error) {
	return wrapper.stream.Read(b)
}

func (wrapper receiveStreamWrapper) CancelRead(code quic.StreamErrorCode) {
	wrapper.stream.CancelRead(quicgo_webtransportgo.StreamErrorCode(code))
}

func (wrapper receiveStreamWrapper) SetReadDeadline(time time.Time) error {
	return wrapper.stream.SetReadDeadline(time)
}

/*
 *
 */
type sendStreamWrapper struct {
	stream *quicgo_webtransportgo.SendStream
}

func (wrapper sendStreamWrapper) Write(b []byte) (int, error) {
	return wrapper.stream.Write(b)
}

func (wrapper sendStreamWrapper) CancelWrite(code quic.StreamErrorCode) {
	wrapper.stream.CancelWrite(quicgo_webtransportgo.StreamErrorCode(code))
}

func (wrapper sendStreamWrapper) SetWriteDeadline(time time.Time) error {
	return wrapper.stream.SetWriteDeadline(time)
}

func (wrapper sendStreamWrapper) Close() error {
	return wrapper.stream.Close()
}

func (wrapper sendStreamWrapper) Context() context.Context {
	return wrapper.stream.Context()
}
