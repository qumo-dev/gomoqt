package moqt

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/qumo-dev/gomoqt/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMockReceiveStreamWithCleanup(tb testing.TB) *FakeQUICReceiveStream {
	tb.Helper()
	return &FakeQUICReceiveStream{}
}

func TestNewReceiveGroupStream(t *testing.T) {
	tests := map[string]struct {
		sequence    GroupSequence
		expectValid bool
	}{
		"valid creation": {
			sequence:    GroupSequence(123),
			expectValid: true,
		},
		"zero sequence": {
			sequence:    GroupSequence(0),
			expectValid: true,
		},
		"large sequence": {
			sequence:    GroupSequence(4294967295),
			expectValid: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := newMockReceiveStreamWithCleanup(t)

			rgs := newGroupReader(tt.sequence, mockStream, nil)

			assert.NotNil(t, rgs)
			assert.Equal(t, tt.sequence, rgs.sequence)
			assert.Equal(t, mockStream, rgs.stream)
			assert.Equal(t, int64(0), rgs.frameCount)
		})
	}
}

func TestReceiveGroupStream_GroupSequence(t *testing.T) {
	tests := map[string]struct {
		sequence GroupSequence
	}{
		"minimum value": {
			sequence: GroupSequence(0),
		},
		"small value": {
			sequence: GroupSequence(1),
		},
		"medium value": {
			sequence: GroupSequence(1000),
		},
		"large value": {
			sequence: GroupSequence(1000000),
		},
		"maximum uint64": {
			sequence: GroupSequence(1<<(64-2) - 1), // maxVarInt8
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := newMockReceiveStreamWithCleanup(t)
			rgs := newGroupReader(tt.sequence, mockStream, nil)

			result := rgs.GroupSequence()
			assert.Equal(t, tt.sequence, result)
		})
	}
}

func TestReceiveGroupStream_ReadFrame_EOF(t *testing.T) {
	mockStream := &FakeQUICReceiveStream{}
	buf := bytes.NewBuffer(nil) // Empty buffer will return EOF
	mockStream.ReadFunc = buf.Read

	rgs := newGroupReader(GroupSequence(123), mockStream, nil)
	frame := NewFrame(0)
	err := rgs.ReadFrame(frame)
	assert.Error(t, err)
	assert.Equal(t, io.EOF, err)
	// ReadFrame doesn't modify frame on error, so frame object should still exist
	assert.NotNil(t, frame)
}

func TestReceiveGroupStream_CancelRead(t *testing.T) {
	tests := map[string]struct {
		errorCode   GroupErrorCode
		withManager bool
	}{
		"internal group error without manager": {
			errorCode:   InternalGroupErrorCode,
			withManager: false,
		},
		"out of range error without manager": {
			errorCode:   OutOfRangeErrorCode,
			withManager: false,
		},
		"expired group error without manager": {
			errorCode:   ExpiredGroupErrorCode,
			withManager: false,
		},
		"internal group error with manager": {
			errorCode:   InternalGroupErrorCode,
			withManager: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := &FakeQUICReceiveStream{}

			var mgr *groupReaderManager
			if tt.withManager {
				mgr = newGroupReaderManager()
			}

			rgs := newGroupReader(GroupSequence(123), mockStream, mgr)

			if tt.withManager {
				assert.Contains(t, mgr.activeGroups, rgs)
			}

			rgs.CancelRead(tt.errorCode)

			_, readErr := mockStream.Read(make([]byte, 1))
			var cancelReadErr *transport.StreamError
			require.ErrorAs(t, readErr, &cancelReadErr)
			assert.Equal(t, transport.StreamErrorCode(tt.errorCode), cancelReadErr.ErrorCode)

			if tt.withManager {
				assert.NotContains(t, mgr.activeGroups, rgs)
			}
		})
	}
}

func TestReceiveGroupStream_CancelRead_MultipleCalls(t *testing.T) {
	mockStream := &FakeQUICReceiveStream{}

	rgs := newGroupReader(GroupSequence(123), mockStream, nil)

	// Cancel multiple times with the same error code
	rgs.CancelRead(InternalGroupErrorCode)
	rgs.CancelRead(InternalGroupErrorCode)

	// Should reflect the last CancelRead invocation
	expected := transport.StreamErrorCode(InternalGroupErrorCode)
	_, readErr := mockStream.Read(make([]byte, 1))
	var cancelReadErr *transport.StreamError
	require.ErrorAs(t, readErr, &cancelReadErr)
	assert.Equal(t, expected, cancelReadErr.ErrorCode)
}

func TestReceiveGroupStream_SetReadDeadline(t *testing.T) {
	tests := map[string]struct {
		setupMock func() *FakeQUICReceiveStream
		deadline  time.Time
		wantErr   bool
	}{
		"successful set deadline": {
			setupMock: func() *FakeQUICReceiveStream {
				return &FakeQUICReceiveStream{}
			},
			deadline: time.Now().Add(time.Hour),
			wantErr:  false,
		},
		"set deadline with error": {
			setupMock: func() *FakeQUICReceiveStream {
				return &FakeQUICReceiveStream{
					SetReadDeadlineFunc: func(t time.Time) error { return assert.AnError },
				}
			},
			deadline: time.Now().Add(time.Hour),
			wantErr:  true,
		},
		"zero time deadline": {
			setupMock: func() *FakeQUICReceiveStream {
				return &FakeQUICReceiveStream{}
			},
			deadline: time.Time{},
			wantErr:  false,
		},
		"deadline in the past": {
			setupMock: func() *FakeQUICReceiveStream {
				return &FakeQUICReceiveStream{}
			},
			deadline: time.Now().Add(-time.Hour),
			wantErr:  false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := tt.setupMock()
			rgs := newGroupReader(123, mockStream, nil)

			err := rgs.SetReadDeadline(tt.deadline)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestReceiveGroupStream_ReadFrame_StreamError(t *testing.T) {
	mockStream := &FakeQUICReceiveStream{
		ReadFunc: func(p []byte) (int, error) {
			return 0, &transport.StreamError{
				StreamID:  transport.StreamID(123),
				ErrorCode: transport.StreamErrorCode(1),
			}
		},
	}

	rgs := newGroupReader(123, mockStream, nil)
	frame := NewFrame(0)
	err := rgs.ReadFrame(frame)
	assert.Error(t, err)
	// ReadFrame doesn't modify frame on error, so frame object should still exist
	assert.NotNil(t, frame)

	// Should be a GroupError
	var groupErr *GroupError
	assert.True(t, errors.As(err, &groupErr))
}

func TestGroupReader_ReadFrame(t *testing.T) {
	tests := map[string]struct {
		setupStream func() *FakeQUICReceiveStream
		expectError bool
		expectFrame bool
	}{
		"successful read": {
			setupStream: func() *FakeQUICReceiveStream {
				// Create a frame with some data
				frame := NewFrame(10)
				_, _ = frame.Write([]byte("test data"))
				var buf bytes.Buffer
				err := frame.encode(&buf)
				if err != nil {
					panic(err)
				}
				data := buf.Bytes()

				mockStream := &FakeQUICReceiveStream{
					ReadFunc: func(p []byte) (int, error) {
						if len(data) == 0 {
							return 0, io.EOF
						}
						n := copy(p, data)
						data = data[n:]
						return n, nil
					},
				}
				return mockStream
			},
			expectError: false,
			expectFrame: true,
		},
		"EOF": {
			setupStream: func() *FakeQUICReceiveStream {
				mockStream := &FakeQUICReceiveStream{
					ReadFunc: func(p []byte) (int, error) {
						return 0, io.EOF
					},
				}
				return mockStream
			},
			expectError: true,
			expectFrame: true, // ReadFrame doesn't modify frame on error
		},
		"generic error": {
			setupStream: func() *FakeQUICReceiveStream {
				mockStream := &FakeQUICReceiveStream{
					ReadFunc: func(p []byte) (int, error) {
						return 0, errors.New("generic decode error")
					},
				}
				return mockStream
			},
			expectError: true,
			expectFrame: true, // ReadFrame doesn't modify frame on error
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockStream := tt.setupStream()
			rgs := newGroupReader(123, mockStream, nil)

			frame := NewFrame(0)
			err := rgs.ReadFrame(frame)
			if tt.expectError {
				assert.Error(t, err)
				if tt.expectFrame {
					assert.NotNil(t, frame)
				} else {
					assert.Nil(t, frame)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, frame)
			}
		})
	}

	t.Run("nil frame panics", func(t *testing.T) {
		mockStream := &FakeQUICReceiveStream{}
		rgs := newGroupReader(123, mockStream, nil)
		assert.PanicsWithValue(t, "nil frame", func() {
			rgs.ReadFrame(nil)
		})
	})
}

func TestGroupReader_Frames(t *testing.T) {
	t.Run("returns iterator function", func(t *testing.T) {
		mockStream := &FakeQUICReceiveStream{
			ReadFunc: func(p []byte) (int, error) {
				return 0, io.EOF
			},
		}

		rgs := newGroupReader(123, mockStream, nil)
		iterator := rgs.Frames(nil)
		assert.NotNil(t, iterator)
	})

	t.Run("iterates frames until error", func(t *testing.T) {
		// Prepare a single encoded frame
		frame := NewFrame(20)
		_, _ = frame.Write([]byte("test"))

		var buf bytes.Buffer
		err := frame.encode(&buf)
		if err != nil {
			t.Fatalf("failed to encode frame: %v", err)
		}

		encodedData := buf.Bytes()

		mockStream := &FakeQUICReceiveStream{
			ReadFunc: func(p []byte) (int, error) {
				if len(encodedData) == 0 {
					return 0, io.EOF
				}
				n := copy(p, encodedData)
				encodedData = encodedData[n:]
				return n, nil
			},
		}

		rgs := newGroupReader(123, mockStream, nil)

		frameCount := 0
		var frames []*Frame
		for frame := range rgs.Frames(nil) {
			frameCount++
			// Clone the frame since GroupReader reuses the same frame object
			frames = append(frames, frame.Clone())
			if frameCount > 1 {
				break
			}
		}

		assert.GreaterOrEqual(t, frameCount, 1)
		// Verify frames are not nil
		for _, f := range frames {
			assert.NotNil(t, f)
		}
	})

	t.Run("stops immediately on EOF", func(t *testing.T) {
		mockStream := &FakeQUICReceiveStream{
			ReadFunc: func(p []byte) (int, error) {
				return 0, io.EOF
			},
		}

		rgs := newGroupReader(123, mockStream, nil)

		frameCount := 0
		for range rgs.Frames(nil) {
			frameCount++
		}

		assert.Equal(t, 0, frameCount)
	})
}
