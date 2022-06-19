package evnet

import (
	"bytes"
	"net"
	"sync"
	"time"
	// "fmt"

	sys "golang.org/x/sys/unix"
)

type conn struct {
	cOnce      sync.Once
	fd         int
	localAddr  net.Addr
	remoteAddr net.Addr
	addr       sys.Sockaddr

	input  chan []byte
	inBuf  *bytes.Buffer
	outBuf *bytes.Buffer

	errChan    chan error
	closeQueue chan *msg
}

func newConn(fd int, localAddr net.Addr, remoteAddr net.Addr, closeQueue chan *msg) *conn {
	return &conn{
		fd:         fd,
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
		input:      make(chan []byte, 1024),
		inBuf:      getBuffer(),
		outBuf:     getBuffer(),
		errChan:    make(chan error, 1),
		closeQueue: closeQueue,
	}
}

func (c *conn) Read(b []byte) (n int, err error) {
	if c.inBuf.Len() > 0 {
		n, err = c.inBuf.Read(b)
		if c.inBuf.Len() == 0 {
			c.inBuf.Reset()
		}
		return
	}

	select {
	case raw := <-c.input:
		{
			c.inBuf.Write(raw)
			n, err = c.inBuf.Read(b)

			if c.inBuf.Len() == 0 {
				c.inBuf.Reset()
			}
		}
	case err = <-c.errChan:
		{
		}
	}
	return
}

func (c *conn) Write(b []byte) (n int, err error) {
	n, err = sys.Write(c.fd, b)
	// if c.outBuf.Len() > 0 {
	// 	return c.outBuf.Write(b)
	// }

	// n, err = sys.Write(c.fd, b)
	// if err == sys.EAGAIN {
	// 	if n < len(b) {
	// 		c.outBuf.Write(b[n:])
	// 	}
	// 	err = nil
	// }
	return
}

func (c *conn) Close() error {
	c.cOnce.Do(func() {
		c.closeQueue <- &msg{action: actionCloseConn, c: c}
		// close(c.input)
		close(c.errChan)
		c.errChan = nil
		putBuffer(c.inBuf)
		putBuffer(c.outBuf)
	})
	return nil
}

func (c *conn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *conn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *conn) SetDeadline(t time.Time) error {
	duration := t.Nanosecond() - time.Now().Nanosecond()

	if duration < 0 {
		sys.SetsockoptTimeval(c.fd, sys.SOL_SOCKET, sys.SO_RCVTIMEO, &sys.Timeval{Sec: 0, Usec: 0})
		sys.SetsockoptTimeval(c.fd, sys.SOL_SOCKET, sys.SO_SNDTIMEO, &sys.Timeval{Sec: 0, Usec: 0})
	} else {
		//read write timeout
		ts := sys.NsecToTimeval(int64(duration))
		sys.SetsockoptTimeval(c.fd, sys.SOL_SOCKET, sys.SO_RCVTIMEO, &ts)
		sys.SetsockoptTimeval(c.fd, sys.SOL_SOCKET, sys.SO_SNDTIMEO, &ts)
	}
	return nil
}

func (c *conn) SetReadDeadline(t time.Time) error {
	duration := t.Nanosecond() - time.Now().Nanosecond()
	//read timeout
	if duration < 0 {
		sys.SetsockoptTimeval(c.fd, sys.SOL_SOCKET, sys.SO_RCVTIMEO, &sys.Timeval{Sec: 0, Usec: 0})
	} else {
		ts := sys.NsecToTimeval(int64(duration))
		sys.SetsockoptTimeval(c.fd, sys.SOL_SOCKET, sys.SO_RCVTIMEO, &ts)
	}
	return nil
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	duration := t.Nanosecond() - time.Now().Nanosecond()
	//write timeout
	if duration < 0 {
		sys.SetsockoptTimeval(c.fd, sys.SOL_SOCKET, sys.SO_SNDTIMEO, &sys.Timeval{Sec: 0, Usec: 0})
	} else {
		ts := sys.NsecToTimeval(int64(duration))
		sys.SetsockoptTimeval(c.fd, sys.SOL_SOCKET, sys.SO_SNDTIMEO, &ts)
	}
	return nil
}

func (c *conn) CloseWrite() error {
	return sys.Shutdown(c.fd, sys.SHUT_WR)
}
