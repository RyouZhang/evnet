package evnet

import (
	"io"
	"net"
	"sync"
)

type listener struct {
	addr net.Addr

	cOnce sync.Once

	acceptQueue chan *conn
	shutdown    chan bool
}

func newListener(addr net.Addr, acceptQueue chan *conn, shutdown chan bool) *listener {
	return &listener{
		addr:        addr,
		shutdown:    shutdown,
		acceptQueue: acceptQueue,
	}
}

func (l *listener) Accept() (net.Conn, error) {
	ec, ok := <-l.acceptQueue
	if ok {
		return ec, nil
	}
	return nil, io.EOF
}

func (l *listener) Close() error {
	l.cOnce.Do(func() {
		close(l.shutdown)
	})
	return nil
}

func (l *listener) Addr() net.Addr {
	return l.addr
}
