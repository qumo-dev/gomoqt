package moqt

import (
	"bytes"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadSubscribeResponse_AllTypes(t *testing.T) {
	tests := map[string]struct {
		build   func(t *testing.T) []byte
		wantOK  bool
		wantEnd bool
		wantErr bool
	}{
		"subscribe ok": {
			build: func(t *testing.T) []byte {
				var b bytes.Buffer
				require.NoError(t, (&message.SubscribeOkMessage{Group: 7}).Encode(&b))
				return append([]byte{byte(message.MessageTypeSubscribeOk)}, b.Bytes()...)
			},
			wantOK: true,
		},
		"subscribe end": {
			build: func(t *testing.T) []byte {
				var b bytes.Buffer
				require.NoError(t, (&message.SubscribeEndMessage{Group: 9}).Encode(&b))
				return append([]byte{byte(message.MessageTypeSubscribeEnd)}, b.Bytes()...)
			},
			wantEnd: true,
		},
		"subscribe drop": {
			build: func(t *testing.T) []byte {
				var b bytes.Buffer
				require.NoError(t, (&message.SubscribeDropMessage{
					GroupStart: 2, GroupEnd: 4, ErrorCode: 3,
				}).Encode(&b))
				return append([]byte{byte(message.MessageTypeSubscribeDrop)}, b.Bytes()...)
			},
		},
		"unknown type": {
			build:   func(t *testing.T) []byte { return []byte{0x09} },
			wantErr: true,
		},
		"truncated ok body": {
			build:   func(t *testing.T) []byte { return []byte{byte(message.MessageTypeSubscribeOk), 0x05} },
			wantErr: true,
		},
		"empty input": {
			build:   func(t *testing.T) []byte { return nil },
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			resp, err := readSubscribeResponse(bytes.NewReader(tt.build(t)))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOK, resp.ok != nil)
			assert.Equal(t, tt.wantEnd, resp.end != nil)
		})
	}
}

// TestSendSubscribeStream_ReadSubscribeResponses_EndAndDrop drives the
// background reader with OK, END, and DROP messages and asserts the
// subscriber-side state transitions (resolvedStart, ended, drops).
func TestSendSubscribeStream_ReadSubscribeResponses_EndAndDrop(t *testing.T) {
	var buf bytes.Buffer

	writeFramed := func(msgType byte, encode func(*bytes.Buffer) error) {
		t.Helper()
		buf.WriteByte(msgType)
		require.NoError(t, encode(&buf))
	}

	writeFramed(byte(message.MessageTypeSubscribeOk),
		func(b *bytes.Buffer) error { return (&message.SubscribeOkMessage{Group: 5}).Encode(b) })
	writeFramed(byte(message.MessageTypeSubscribeEnd),
		func(b *bytes.Buffer) error { return (&message.SubscribeEndMessage{Group: 11}).Encode(b) })
	writeFramed(byte(message.MessageTypeSubscribeDrop),
		func(b *bytes.Buffer) error {
			return (&message.SubscribeDropMessage{GroupStart: 2, GroupEnd: 3, ErrorCode: 4}).Encode(b)
		})

	stream := &FakeQUICStream{ReadFunc: bytes.NewReader(buf.Bytes()).Read}
	substr := newSendSubscribeStream(SubscribeID(1), stream, &SubscribeConfig{})

	done := make(chan struct{})
	go func() {
		substr.readSubscribeResponses()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readSubscribeResponses did not drain the stream")
	}

	assert.True(t, substr.okReceived, "OK should mark okReceived")
	assert.Equal(t, GroupSequence(5), substr.resolvedStart)
	assert.True(t, substr.ended, "END should mark ended")
	assert.Equal(t, GroupSequence(11), substr.endGroup)

	drops := substr.pendingDrops()
	require.Len(t, drops, 1)
	assert.Equal(t, SubscribeDrop{StartGroup: 2, EndGroup: 3, ErrorCode: 4}, drops[0])
}

// TestSendSubscribeStream_SetEnd_Direct exercises the setEnd path (used when
// the first publisher response is SUBSCRIBE_END with no preceding OK).
func TestSendSubscribeStream_SetEnd_Direct(t *testing.T) {
	stream := &FakeQUICStream{}
	substr := newSendSubscribeStream(SubscribeID(2), stream, &SubscribeConfig{})

	substr.setEnd(GroupSequence(42))

	assert.True(t, substr.ended)
	assert.Equal(t, GroupSequence(42), substr.endGroup)
	assert.False(t, substr.okReceived, "setEnd alone must not imply OK")
}
