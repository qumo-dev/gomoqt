package moqt

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"sync"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/qumo-dev/gomoqt/moqttrace"
	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
)

func TestSessionTraceHooks(t *testing.T) {
	t.Run("SubscribeSent", func(t *testing.T) {
		var mu sync.Mutex
		var events []string
		trace := &moqttrace.SessionTrace{
			SubscribeSent: func(info moqttrace.SubscribeRequestInfo) {
				mu.Lock()
				events = append(events, "SubscribeSent")
				mu.Unlock()
			},
			SubscribeSentError: func(info moqttrace.SubscribeRequestInfo, err error) {
				mu.Lock()
				events = append(events, "SubscribeSentError")
				mu.Unlock()
			},
		}
		ctx := moqttrace.ContextWithSessionTrace(context.Background(), trace)
		conn := &FakeStreamConn{ParentCtx: ctx}
		conn.TLSFunc = func() *tls.ConnectionState { return &tls.ConnectionState{} }
		session := newSession(conn, nil, nil, nil, nil, nil, nil)

		conn.OpenStreamFunc = func() (transport.Stream, error) {
			return nil, errors.New("open error")
		}
		_, err := session.Subscribe(context.Background(), "/path", "name", nil)
		assert.Error(t, err)

		mu.Lock()
		assert.Contains(t, events, "SubscribeSent")
		assert.Contains(t, events, "SubscribeSentError")
		mu.Unlock()
	})

	t.Run("FetchSent", func(t *testing.T) {
		var mu sync.Mutex
		var events []string
		trace := &moqttrace.SessionTrace{
			FetchSent: func(info moqttrace.FetchRequestInfo) {
				mu.Lock()
				events = append(events, "FetchSent")
				mu.Unlock()
			},
		}
		ctx := moqttrace.ContextWithSessionTrace(context.Background(), trace)
		conn := &FakeStreamConn{ParentCtx: ctx}
		conn.TLSFunc = func() *tls.ConnectionState { return &tls.ConnectionState{} }
		session := newSession(conn, nil, nil, nil, nil, nil, nil)

		conn.OpenStreamFunc = func() (transport.Stream, error) {
			return nil, errors.New("open error")
		}
		_, err := session.Fetch(&FetchRequest{
			BroadcastPath: "/path",
			TrackName:     "name",
		})
		assert.Error(t, err)

		mu.Lock()
		assert.Contains(t, events, "FetchSent")
		mu.Unlock()
	})

	t.Run("SessionClosed", func(t *testing.T) {
		var mu sync.Mutex
		var events []string
		trace := &moqttrace.SessionTrace{
			SessionClosed: func(info moqttrace.SessionCloseInfo) {
				mu.Lock()
				events = append(events, "SessionClosed")
				mu.Unlock()
			},
		}
		ctx := moqttrace.ContextWithSessionTrace(context.Background(), trace)
		conn := &FakeStreamConn{ParentCtx: ctx}
		conn.TLSFunc = func() *tls.ConnectionState { return &tls.ConnectionState{} }
		session := newSession(conn, nil, nil, nil, nil, nil, nil)

		conn.CloseWithErrorFunc = func(code transport.ConnErrorCode, msg string) error {
			return nil
		}
		err := session.CloseWithError(NoError, "closing")
		assert.NoError(t, err)

		mu.Lock()
		assert.Contains(t, events, "SessionClosed")
		mu.Unlock()
	})

	t.Run("SubscribeAccepted", func(t *testing.T) {
		var acceptedID uint64
		var wg sync.WaitGroup
		wg.Add(1)
		trace := &moqttrace.SessionTrace{
			SubscribeAccepted: func(info moqttrace.SubscribeInfo) {
				acceptedID = info.SubscribeID
				wg.Done()
			},
		}
		ctx := moqttrace.ContextWithSessionTrace(context.Background(), trace)
		conn := &FakeStreamConn{ParentCtx: ctx}
		conn.TLSFunc = func() *tls.ConnectionState { return &tls.ConnectionState{} }
		session := newSession(conn, nil, nil, nil, nil, nil, nil)

		// Mock stream that returns SUBSCRIBE_OK
		mockStream := &FakeQUICStream{}
		msg := message.SubscribeOkMessage{
			PublisherPriority: 10,
		}
		var buf bytes.Buffer
		// readSubscribeResponse starts by reading msg type.
		_, _ = buf.Write([]byte{byte(message.MessageTypeSubscribeOk)})
		_ = msg.Encode(&buf)
		mockStream.ReadFunc = buf.Read

		conn.OpenStreamFunc = func() (transport.Stream, error) {
			return mockStream, nil
		}

		_, err := session.Subscribe(context.Background(), "/path", "name", nil)
		assert.NoError(t, err)

		wg.Wait()
		assert.NotZero(t, acceptedID)
	})
}
