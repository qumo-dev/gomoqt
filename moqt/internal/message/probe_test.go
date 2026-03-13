package message_test

import (
	"bytes"
	"testing"

	"github.com/okdaichi/gomoqt/moqt/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeMessage_EncodeDecode(t *testing.T) {
	tests := map[string]struct {
		input   message.ProbeMessage
		wantErr bool
	}{
		"valid_message": {
			input: message.ProbeMessage{Bitrate: 1000000},
		},
		"zero_bitrate": {
			input: message.ProbeMessage{Bitrate: 0},
		},
		"large_bitrate": {
			input: message.ProbeMessage{Bitrate: 1 << 48},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer

			// Encode
			err := tc.input.Encode(&buf)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Decode
			var decoded message.ProbeMessage
			err = decoded.Decode(&buf)
			require.NoError(t, err)

			// Compare fields
			assert.Equal(t, tc.input, decoded, "decoded message should match input")
		})
	}
}

func TestProbeMessage_DecodeErrors(t *testing.T) {
	tests := map[string]struct {
		data    []byte
		wantErr bool
	}{
		"empty_reader": {
			data:    []byte{},
			wantErr: true,
		},
		"truncated_length": {
			data:    []byte{0xff}, // incomplete varint for length
			wantErr: true,
		},
		"truncated_bitrate": {
			data:    []byte{0x02, 0xff}, // length=2 but bitrate incomplete
			wantErr: true,
		},
		"extra_data": {
			data:    []byte{0x02, 0xe8, 0x07, 0xff}, // length=2, bitrate=1000, extra=0xff
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			buf := bytes.NewReader(tc.data)
			var pm message.ProbeMessage
			err := pm.Decode(buf)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
