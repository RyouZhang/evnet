package evnet

import (
	"bytes"
	"fmt"
	// "io"
	// "errors"
	"net"
	"sync"
	// "syscall"
	"time"

	sys "golang.org/x/sys/unix"
)

type conn struct {
	cOnce      sync.Once
	fd         int
	localAddr  net.Addr
	remoteAddr net.Addr
	addr       sys.Sockaddr

	input      chan []byte
	inBuf      *bytes.Buffer
	errChan    chan error
	closeQueue chan *conn
}

func newConn(fd int, localAddr net.Addr, remoteAddr net.Addr) *conn {
	return &conn{
		fd:         fd,
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
		input:      make(chan []byte, 64),
		inBuf:      bytes.NewBuffer(make([]byte, 0, 4096)),
		errChan:    make(chan error, 4),
	}
}

func (ec *conn) Read(b []byte) (n int, err error) {
	if ec.inBuf.Len() > 0 {
		n, err = ec.inBuf.Read(b)
		if ec.inBuf.Len() == 0 {
			ec.inBuf.Reset()
		}
		return
	}

	select {
	case raw := <-ec.input:
		{
			ec.inBuf.Write(raw)
			n, err = ec.inBuf.Read(b)

			if ec.inBuf.Len() == 0 {
				ec.inBuf.Reset()
			}
		}
	case err = <-ec.errChan:
		{
			fmt.Println("read:", err)
		}
	}
	return
}

func (ec *conn) Write(b []byte) (n int, err error) {
	n, err = sys.Write(ec.fd, b)
	return
}

func (ec *conn) Close() error {
	fmt.Println("conn Close", ec.fd)
	ec.cOnce.Do(func() {
		ec.closeQueue <- ec
		close(ec.input)
	})
	return nil
}

func (ec *conn) LocalAddr() net.Addr {
	return ec.localAddr
}

func (ec *conn) RemoteAddr() net.Addr {
	return ec.remoteAddr
}

func (ec *conn) SetDeadline(t time.Time) error {
	return nil
}

func (ec *conn) SetReadDeadline(t time.Time) error {
	return nil
}

func (ec *conn) SetWriteDeadline(t time.Time) error {
	return nil
}
