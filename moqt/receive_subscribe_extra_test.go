package moqt

import (
	"bytes"
	"testing"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newBufReceiveStream builds a receiveSubscribeStream whose writes are
// captured in a buffer for decoding back into typed messages.
func newBufReceiveStream(t *testing.T) (*receiveSubscribeStream, *bytes.Buffer) {
	t.Helper()
	mockStream := &FakeQUICStream{}
	var buf bytes.Buffer
	mockStream.WriteFunc = buf.Write
	return newReceiveSubscribeStream(SubscribeID(1), mockStream, &SubscribeConfig{}), &buf
}

func TestReceiveSubscribeStream_WriteEnd_Idempotent(t *testing.T) {
	substr, buf := newBufReceiveStream(t)

	require.NoError(t, substr.writeEnd(7))
	firstLen := buf.Len()
	require.NoError(t, substr.writeEnd(8))
	assert.Equal(t, firstLen, buf.Len(), "second writeEnd must be a no-op")

	// Skip the type tag the caller would have written; decode the body.
	b := buf.Bytes()
	var end message.SubscribeEndMessage
	require.NoError(t, end.Decode(bytes.NewReader(b[1:])))
	assert.Equal(t, uint64(7), end.Group)
}

func TestReceiveSubscribeStream_WriteDrop_AfterOk(t *testing.T) {
	substr, buf := newBufReceiveStream(t)

	// SUBSCRIBE_OK already sent, so writeDrop emits an explicit DROP
	// (not the implicit leading-range OK shortcut).
	require.NoError(t, substr.ensureOk(3))
	require.NoError(t, substr.writeDrop(SubscribeDrop{StartGroup: 4, EndGroup: 6, ErrorCode: 2}))

	b := buf.Bytes()
	// [OK type][OK msglen][OK group=3][DROP type][DROP body...]
	assert.Equal(t, byte(message.MessageTypeSubscribeOk), b[0])
	assert.Equal(t, byte(message.MessageTypeSubscribeDrop), b[3])
	var drop message.SubscribeDropMessage
	require.NoError(t, drop.Decode(bytes.NewReader(b[4:])))
	assert.Equal(t, uint64(4), drop.GroupStart)
	assert.Equal(t, uint64(6), drop.GroupEnd)
	assert.Equal(t, uint64(2), drop.ErrorCode)
}
