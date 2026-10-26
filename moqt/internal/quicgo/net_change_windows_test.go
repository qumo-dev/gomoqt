//go:build windows

package quicgo

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows"
)

func TestIsNetChangeErr(t *testing.T) {
	// sendErr is a send error as the net package returns it.
	sendErr := func(errno syscall.Errno) error {
		return &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("wsasendto", errno)}
	}
	tests := map[string]struct {
		err  error
		want bool
	}{
		"network unreachable":         {err: sendErr(windows.WSAENETUNREACH), want: true},
		"host unreachable":            {err: sendErr(windows.WSAEHOSTUNREACH), want: true},
		"network down":                {err: sendErr(windows.WSAENETDOWN), want: true},
		"wrapped network unreachable": {err: fmt.Errorf("send: %w", sendErr(windows.WSAENETUNREACH)), want: true},
		"message too large":           {err: sendErr(windows.WSAEMSGSIZE)},
		"connection reset":            {err: sendErr(windows.WSAECONNRESET)},
		"socket closed":               {err: net.ErrClosed},
		"error with no errno":         {err: errors.New("boom")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, isNetChangeErr(tt.err))
		})
	}
}
