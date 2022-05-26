package next

import (
	"bytes"
	"errors"
	// "fmt"
	"net"
	"sync"
	"time"

	gnet "github.com/panjf2000/gnet/v2"
)

var bufPool = sync.Pool{}

type conn struct {
	c     gnet.Conn
	cOnce sync.Once

	isError bool

	errChan   chan error
	shutdown  chan bool

	buf *bytes.Buffer

	rDeadline time.Time
}

func newEvConn(c gnet.Conn) *conn {
	buf, _ := bufPool.Get().(*bytes.Buffer)
	if buf == nil {
		buf = bytes.NewBuffer(make([]byte, 0, 4096))
	} else {
		buf.Reset()
	}
	evc := &conn{
		c:        c,
		errChan:  make(chan error, 1),
		shutdown: make(chan bool),
		buf:      buf,
	}
	c.SetContext(evc)

	return evc
}

func (ec *conn) getError() error {
	select {
	case err := <-ec.errChan:
		{
			ec.isError = true
			if err == nil {
				return errors.New("broken pipe")
			}
			return err
		}
	default:
		return nil
	}
}

func (ec *conn) Read(b []byte) (n int, err error) {
	err = ec.getError()
	if err != nil {
		return
	}

	if ec.buf.Len() > 0 {
		n, err = ec.buf.Read(b)
		if err != nil {
			return
		}
		if ec.buf.Len() == 0 {
			ec.buf.Reset()		
		}
		return
	}

	raw, err := ec.c.Next(-1)
	if err != nil {
		return 0, err
	}
	ec.buf.Write(raw)
	n, err = ec.buf.Read(b)
	if err != nil {
		return
	}
	if ec.buf.Len() == 0 {
		ec.buf.Reset()		
	}
	return
}

func (ec *conn) Write(b []byte) (n int, err error) {
	err = ec.getError()
	if err != nil {
		return
	}
	return ec.c.Write(b)
}

func (ec *conn) Close() error {
	err := ec.getError()
	if err != nil {
		return err
	}
	return ec.c.Close()
}

func (ec *conn) LocalAddr() net.Addr {
	return ec.c.LocalAddr()
}

func (ec *conn) RemoteAddr() net.Addr {
	return ec.c.RemoteAddr()
}

func (ec *conn) SetDeadline(t time.Time) error {
	ec.rDeadline = t
	return ec.c.SetDeadline(t)
}

func (ec *conn) SetReadDeadline(t time.Time) error {
	ec.rDeadline = t
	return ec.c.SetReadDeadline(t)
}

func (ec *conn) SetWriteDeadline(t time.Time) error {
	return ec.c.SetWriteDeadline(t)
}
