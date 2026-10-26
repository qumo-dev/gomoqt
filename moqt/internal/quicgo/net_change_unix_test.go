//go:build unix

package quicgo

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsNetChangeErr(t *testing.T) {
	// sendErr is a send error as the net package returns it.
	sendErr := func(errno syscall.Errno) error {
		return &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendmsg", errno)}
	}
	tests := map[string]struct {
		err  error
		want bool
	}{
		"network unreachable":         {err: sendErr(syscall.ENETUNREACH), want: true},
		"host unreachable":            {err: sendErr(syscall.EHOSTUNREACH), want: true},
		"network down":                {err: sendErr(syscall.ENETDOWN), want: true},
		"wrapped network unreachable": {err: fmt.Errorf("send: %w", sendErr(syscall.ENETUNREACH)), want: true},
		"message too large":           {err: sendErr(syscall.EMSGSIZE)},
		"connection refused":          {err: sendErr(syscall.ECONNREFUSED)},
		"socket closed":               {err: net.ErrClosed},
		"permission denied":           {err: sendErr(syscall.EPERM)},
		"error with no errno":         {err: errors.New("boom")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, isNetChangeErr(tt.err))
		})
	}
}
