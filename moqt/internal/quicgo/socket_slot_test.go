package quicgo

import (
	"errors"
	"net"
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
			slot := NewSlot(&fakePacketConn{writeErrs: tt.writeErrs})
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
	slot := NewSlot(&fakePacketConn{})
	require.NoError(t, slot.Close())

	_, err := slot.WriteTo([]byte("data"), &net.UDPAddr{})

	assert.ErrorIs(t, err, net.ErrClosed)
}

func TestSocketSlot_Clear(t *testing.T) {
	conn := &fakePacketConn{}
	slot := NewSlot(conn)

	slot.Clear()

	assert.True(t, conn.isClosed(), "a cleared slot closes its socket")
	assert.Equal(t, conn.LocalAddr(), slot.LocalAddr(), "the address is kept while empty")
}

func TestSocketSlot_Set_Closed(t *testing.T) {
	slot := NewSlot(&fakePacketConn{})
	require.NoError(t, slot.Close())
	conn := &fakePacketConn{}

	slot.Set(conn)

	assert.True(t, conn.isClosed(), "a closed slot closes the socket it is given")
}

// A read on an empty slot waits for the next socket and reads from it.
func TestSocketSlot_ReadFrom_WaitsForNextSocket(t *testing.T) {
	slot := NewSlot(&fakePacketConn{})
	slot.Clear()
	next := listenLoopback(t)
	type result struct {
		data string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		buf := make([]byte, 16)
		n, _, err := slot.ReadFrom(buf)
		got <- result{string(buf[:n]), err}
	}()

	slot.Set(next)
	sendTo(t, next.LocalAddr(), "hello")

	select {
	case r := <-got:
		assert.NoError(t, r.err)
		assert.Equal(t, "hello", r.data)
	case <-time.After(3 * time.Second):
		t.Fatal("the read did not move to the next socket")
	}
}

// A socket closed under the slot, not by it, is replaced by one bound to no
// network, so the path keeps sending and receiving instead of going silent.
func TestSocketSlot_ReadFrom_SocketClosedUnderSlot(t *testing.T) {
	first := listenLoopback(t)
	slot := NewSlot(first)
	t.Cleanup(func() { _ = slot.Close() }) // not actionable: cleanup
	got := make(chan error, 1)
	go func() {
		_, _, err := slot.ReadFrom(make([]byte, 16))
		got <- err
	}()

	require.NoError(t, first.Close())

	require.Eventually(t, func() bool {
		return slot.LocalAddr().String() != first.LocalAddr().String()
	}, 3*time.Second, 10*time.Millisecond, "the slot should rebind to a new socket")
	port := slot.LocalAddr().(*net.UDPAddr).Port
	sendTo(t, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}, "after")
	select {
	case err := <-got:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("the read did not move to the new socket")
	}
}

// Buffer sizes quic-go sets reach the socket, and every socket put in the
// slot after it.
func TestSocketSlot_SetBuffers(t *testing.T) {
	first := &fakePacketConn{}
	slot := NewSlot(first)

	require.NoError(t, slot.SetReadBuffer(7<<20))
	require.NoError(t, slot.SetWriteBuffer(7<<20))
	next := &fakePacketConn{}
	slot.Set(next)

	for name, conn := range map[string]*fakePacketConn{"first socket": first, "next socket": next} {
		read, write := conn.buffers()
		assert.Equal(t, 7<<20, read, name)
		assert.Equal(t, 7<<20, write, name)
	}
}

func TestSocketSlot_ReadFrom_EndsWhenClosed(t *testing.T) {
	slot := NewSlot(&fakePacketConn{})
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

func sendTo(tb testing.TB, addr net.Addr, msg string) {
	tb.Helper()
	conn := listenLoopback(tb)
	_, err := conn.WriteTo([]byte(msg), addr)
	require.NoError(tb, err)
}
