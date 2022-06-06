package evnet

import (
	"io"
	"net"
	"sync"
)

type listener struct {
	addr      net.Addr
	shutdown  chan bool
	cOnce     sync.Once
	connQueue chan *conn
}

func newListener(addr net.Addr, connQueue chan *conn) (*listener, error) {
	return &listener{
		addr:      addr,
		shutdown:  make(chan bool, 1),
		connQueue: connQueue,
	}, nil
}

func (l *listener) Accept() (net.Conn, error) {
	ec, ok := <-l.connQueue
	if ok {
		return ec, nil
	}
	return nil, io.EOF
}

func (l *listener) Close() error {
	close(l.shutdown)
	return nil
}

func (l *listener) Addr() net.Addr {
	return l.addr
}
