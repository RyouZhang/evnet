package evnet

import (
	"io"
	"net"
	"sync"

	"github.com/tidwall/evio"
)

type listener struct {
	cOnce sync.Once

	connQueue chan *conn
	shutdown  chan bool

	info evio.Server
}

func newListener(info evio.Server) *listener {
	return &listener{
		shutdown:  make(chan bool),
		connQueue: make(chan *conn, 1024),
		info:      info,
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
		close(l.shutdown)
	})
	return nil
}

func (l *listener) Addr() net.Addr {
	if len(l.info.Addrs) > 0 {
		return l.info.Addrs[0]
	}
	return nil
}
