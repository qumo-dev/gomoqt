package transport

import (
	"context"
	"net"
)

type QUICListener interface {
	Accept(ctx context.Context) (StreamConn, error)
	Close() error
	Addr() net.Addr
}
