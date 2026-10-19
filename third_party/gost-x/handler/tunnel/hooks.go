package tunnel

import (
	"context"
	"github.com/go-gost/core/handler"
	"net"
)

// Hooks extends admission/lifecycle without changing the GOST wire protocol.
// Admit must return a connection whose Close releases its admission record.
type Hooks interface {
	Admit(context.Context, string, string, uint8, string, string, net.Conn) (net.Conn, error)
	Check(net.Conn) error
	Connected(net.Conn)
}

func NewHandlerWithHooks(hooks Hooks, opts ...handler.Option) handler.Handler {
	h := NewHandler(opts...).(*tunnelHandler)
	h.hooks = hooks
	return h
}
