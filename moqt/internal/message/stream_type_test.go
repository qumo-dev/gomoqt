package message_test

import (
	"bytes"
	"io"
	"testing"
	"testing/iotest"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamTypeMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input   message.StreamType
		wantErr bool
	}{
		"valid message": {
			input: message.StreamType(0),
		},
		"max value": {
			input: message.StreamType(^byte(0)),
		},
		"middle value": {
			input: message.StreamType(uint32(42)),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer

			// Encode
			err := tc.input.Encode(&buf)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Decode
			var decoded message.StreamType
			err = decoded.Decode(&buf)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.input, decoded, "decoded message should match input")
		})
	}
}

func TestStreamType_Constants(t *testing.T) {
	tests := map[string]struct {
		streamType message.StreamType
		expected   message.StreamType
	}{
		"announce constant": {
			streamType: message.StreamTypeAnnounce,
			expected:   message.StreamType(0x1),
		},
		"subscribe constant": {
			streamType: message.StreamTypeSubscribe,
			expected:   message.StreamType(0x2),
		},
		"group constant": {
			streamType: message.StreamTypeGroup,
			expected:   message.StreamType(0x0),
		},
		"fetch constant": {
			streamType: message.StreamTypeFetch,
			expected:   message.StreamType(0x3),
		},
		"probe constant": {
			streamType: message.StreamTypeProbe,
			expected:   message.StreamType(0x4),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.streamType)
		})
	}
}

func TestStreamType_DecodeErrors(t *testing.T) {
	tests := map[string]struct {
		src  io.Reader
		want error
	}{
		"empty stream": {src: bytes.NewReader(nil), want: io.EOF},
		"read error":   {src: iotest.ErrReader(iotest.ErrTimeout), want: iotest.ErrTimeout},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var stm message.StreamType
			err := stm.Decode(tt.src)
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

// TestStreamType_Decode_ReaderBehaviors ensures the type byte is decoded however
// the transport delivers it, including in the same Read that reports io.EOF,
// which the io.Reader contract allows and a stream carrying only its type byte
// produces.
func TestStreamType_Decode_ReaderBehaviors(t *testing.T) {
	tests := map[string]io.Reader{
		"whole read":        bytes.NewReader([]byte{byte(message.StreamTypeSubscribe)}),
		"data with io.EOF":  iotest.DataErrReader(bytes.NewReader([]byte{byte(message.StreamTypeSubscribe)})),
		"one byte per read": iotest.OneByteReader(bytes.NewReader([]byte{byte(message.StreamTypeSubscribe)})),
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			var stm message.StreamType
			require.NoError(t, stm.Decode(src))
			assert.Equal(t, message.StreamTypeSubscribe, stm)
		})
	}
}
