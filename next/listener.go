package next

import (
	"fmt"
	"io"
	"net"
	"sync"
)

type listener struct {
	cOnce sync.Once

	addr net.Addr

	connQueue chan net.Conn
	shutdown  chan bool
}

func newListener(addr net.Addr) *listener {
	return &listener{
		shutdown:  make(chan bool),
		connQueue: make(chan net.Conn, 128),
		addr:      addr,
	}
}

func (l *listener) Accept() (net.Conn, error) {
	ec, ok := <-l.connQueue
	if ok {
		return ec, nil
	}
	return nil, io.EOF
}

func (l *listener) Close() error {
	l.cOnce.Do(func() {
		fmt.Println("listen close")
		close(l.shutdown)
	})
	return nil
}

func (l *listener) Addr() net.Addr {
	return l.addr
}
