package quicgo

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// sendErr is a send error as the net package returns it.
func sendErr(errno error) error {
	return &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendmsg", errno)}
}

func TestIsNetChangeErr(t *testing.T) {
	for _, errno := range netChangeErrnos {
		t.Run(errno.Error(), func(t *testing.T) {
			assert.True(t, isNetChangeErr(sendErr(errno)))
			assert.True(t, isNetChangeErr(fmt.Errorf("send: %w", sendErr(errno))), "wrapped")
		})
	}
	tests := map[string]error{
		"socket closed":       net.ErrClosed,
		"error with no errno": errors.New("boom"),
	}
	for name, err := range tests {
		t.Run(name, func(t *testing.T) {
			assert.False(t, isNetChangeErr(err))
		})
	}
}

// A refusal for a network change counts as a lost packet only once the
// connection is established: until then it fails the dial at once.
func TestNetChangeTolerantConn_Lost(t *testing.T) {
	if len(netChangeErrnos) == 0 {
		t.Skip("no network-change errors known on this OS")
	}
	netChange := sendErr(netChangeErrnos[0])
	tests := map[string]struct {
		established bool
		err         error
		want        bool
	}{
		"network change, established": {established: true, err: netChange, want: true},
		"network change, during dial": {err: netChange},
		"other error, established":    {established: true, err: net.ErrClosed},
		"no error, established":       {established: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var established atomic.Bool
			established.Store(tt.established)
			c := netChangeTolerantConn{established: &established}

			assert.Equal(t, tt.want, c.lost(tt.err))
		})
	}
}
