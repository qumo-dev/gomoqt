package quicgo

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSocketSlot_WriteTo(t *testing.T) {
	boom := errors.New("boom")
	tests := map[string]struct {
		writeErrs []error
		empty     bool
		wantErr   error
	}{
		"sent":                    {},
		"empty slot drops":        {empty: true},
		"closed socket drops":     {writeErrs: []error{net.ErrClosed}},
		"other error is reported": {writeErrs: []error{boom}, wantErr: boom},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			slot := newSlot(&fakePacketConn{writeErrs: tt.writeErrs})
			if tt.empty {
				slot.Clear()
			}

			n, err := slot.WriteTo([]byte("data"), &net.UDPAddr{})

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, 4, n)
		})
	}
}

func TestSocketSlot_WriteTo_Closed(t *testing.T) {
	slot := newSlot(&fakePacketConn{})
	require.NoError(t, slot.Close())

	_, err := slot.WriteTo([]byte("data"), &net.UDPAddr{})

	assert.ErrorIs(t, err, net.ErrClosed)
}

func TestSocketSlot_Clear(t *testing.T) {
	conn := &fakePacketConn{}
	slot := newSlot(conn)

	slot.Clear()

	assert.True(t, conn.isClosed(), "a cleared slot closes its socket")
	assert.Equal(t, conn.LocalAddr(), slot.LocalAddr(), "the address is kept while empty")
}

// A socket closed under the slot, not by it, leaves the slot empty, not
// failed: writes are lost, and reads wait until the slot is closed.
func TestSocketSlot_SocketClosedUnderSlot(t *testing.T) {
	sock := listenLoopback(t)
	slot := newSlot(sock)
	got := make(chan error, 1)
	go func() {
		_, _, err := slot.ReadFrom(make([]byte, 16))
		got <- err
	}()

	require.NoError(t, sock.Close())

	n, err := slot.WriteTo([]byte("data"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9})
	assert.NoError(t, err, "a write to an empty slot is lost, not failed")
	assert.Equal(t, 4, n)
	select {
	case err := <-got:
		t.Fatalf("the read ended before the slot closed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, slot.Close())
	select {
	case err := <-got:
		assert.ErrorIs(t, err, net.ErrClosed)
	case <-time.After(3 * time.Second):
		t.Fatal("the read did not end with the slot")
	}
}

// A read deadline reaches the reader, as quic.Transport.Close needs to end
// its reads.
func TestSocketSlot_ReadFrom_Deadline(t *testing.T) {
	slot := newSlot(listenLoopback(t))
	got := make(chan error, 1)
	go func() {
		_, _, err := slot.ReadFrom(make([]byte, 16))
		got <- err
	}()

	require.NoError(t, slot.SetReadDeadline(time.Now()))

	select {
	case err := <-got:
		assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
	case <-time.After(3 * time.Second):
		t.Fatal("the read did not end at its deadline")
	}
}

// Buffer sizes quic-go sets reach the slot's socket.
func TestSocketSlot_SetBuffers(t *testing.T) {
	conn := &fakePacketConn{}
	slot := newSlot(conn)

	require.NoError(t, slot.SetReadBuffer(7<<20))
	require.NoError(t, slot.SetWriteBuffer(7<<20))

	read, write := conn.buffers()
	assert.Equal(t, 7<<20, read)
	assert.Equal(t, 7<<20, write)
}

func TestSocketSlot_ReadFrom_EndsWhenClosed(t *testing.T) {
	slot := newSlot(&fakePacketConn{})
	got := make(chan error, 1)
	go func() {
		_, _, err := slot.ReadFrom(make([]byte, 16))
		got <- err
	}()

	require.NoError(t, slot.Close())

	select {
	case err := <-got:
		assert.ErrorIs(t, err, net.ErrClosed)
	case <-time.After(3 * time.Second):
		t.Fatal("the read did not end with the slot")
	}
}

func listenLoopback(tb testing.TB) *net.UDPConn {
	tb.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = conn.Close() }) // not actionable: cleanup
	return conn
}
