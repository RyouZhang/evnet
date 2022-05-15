package evnet

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/tidwall/evio"
)

var bufPool = sync.Pool{}

type conn struct {
	c     evio.Conn
	cOnce sync.Once

	isError bool

	errChan   chan error
	shutdown  chan bool
	recvQueue chan []byte
	sendQueue chan []byte

	buf *bytes.Buffer

	rDeadline time.Time
	wDeadline time.Time
}

func newEvConn(c evio.Conn, bufferSize int) *conn {
	buf, _ := bufPool.Get().(*bytes.Buffer)
	if buf == nil {
		buf = bytes.NewBuffer(make([]byte, 0, bufferSize))
	} else {
		buf.Reset()
	}

	evc := &conn{
		c:         c,
		errChan:   make(chan error, 1),
		shutdown:  make(chan bool),
		recvQueue: make(chan []byte, 32),
		sendQueue: make(chan []byte, 32),
		buf:       buf,
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
		if ec.buf.Len() == 0 {
			ec.buf.Reset()
		}
		return
	}

	timeout := ec.rDeadline.Sub(time.Now())
	if timeout < 0 {
		select {
		case raw := <-ec.recvQueue:
			{
				ec.buf.Write(raw)
				n, err = ec.buf.Read(b)
				if ec.buf.Len() == 0 {
					ec.buf.Reset()
				}
			}
		case <-ec.shutdown:
			{

			}
		}
		return
	}

	timer := time.NewTimer(timeout)
	select {
	case <-ec.shutdown:
		{
			timer.Stop()
		}
	case raw := <-ec.recvQueue:
		{
			timer.Stop()
			ec.buf.Write(raw)
			n, err = ec.buf.Read(b)
			if ec.buf.Len() == 0 {
				ec.buf.Reset()
			}
		}
	case <-timer.C:
		{
			err = &net.OpError{Op: "read", Addr: ec.LocalAddr(), Source: ec.RemoteAddr(), Err: fmt.Errorf("timeout")}
		}
	}
	return
}

func (ec *conn) Write(b []byte) (n int, err error) {
	err = ec.getError()
	if err != nil {
		return
	}

	select {
	case <-ec.shutdown:
		{
		}
	case ec.sendQueue <- b:
		{
			n = len(b)
			ec.c.Wake()
		}
	}
	return
}

func (ec *conn) Close() error {
	ec.cOnce.Do(func() {
		close(ec.shutdown)
		if !ec.isError {
			ec.c.Wake()
		}
		close(ec.sendQueue)
		bufPool.Put(ec.buf)
	})
	return nil
}

func (ec *conn) LocalAddr() net.Addr {
	return ec.c.LocalAddr()
}

func (ec *conn) RemoteAddr() net.Addr {
	return ec.c.RemoteAddr()
}

func (ec *conn) SetDeadline(t time.Time) error {
	ec.rDeadline = t
	ec.wDeadline = t
	return nil
}

func (ec *conn) SetReadDeadline(t time.Time) error {
	ec.rDeadline = t
	return nil
}

func (ec *conn) SetWriteDeadline(t time.Time) error {
	ec.wDeadline = t
	return nil
}
