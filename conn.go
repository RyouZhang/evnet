package evnet

import (
	"bytes"
	// "fmt"
	// "io"
	"net"
	"syscall"
	"time"
)

type conn struct {
	epfd       int
	fd         int
	localAddr  net.Addr
	remoteAddr net.Addr
	addr       syscall.Sockaddr

	input  chan []byte
	output chan []byte

	inBuf  *bytes.Buffer
	outBuf *bytes.Buffer

	errChan chan error
}

func newConn(fd int, epfd int, localAddr net.Addr, remoteAddr net.Addr) *conn {
	return &conn{
		fd:         fd,
		epfd:       epfd,
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
		errChan:    make(chan error, 1),
		input:      make(chan []byte, 64),
		output:     make(chan []byte, 64),
		inBuf:      bytes.NewBuffer(make([]byte, 0, 4096)),
		outBuf:     bytes.NewBuffer(make([]byte, 0, 4096)),
	}
}
func (ec *conn) Read(b []byte) (n int, err error) {
	// n, err = ec.inBuf.Read(b)
	// if n == 0 {
	// 	return 0, nil
	// }
	// fmt.Println(n, err)
	// return

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
	}
	return
}

func (ec *conn) Write(b []byte) (n int, err error) {
	ec.output <- b
	n = len(b)

	// if ec.outBuf.Len() == 0 {
	// 	ec.outBuf.Reset()
	// }

	// n, err = ec.outBuf.Write(b)
	syscall.EpollCtl(ec.epfd, syscall.EPOLL_CTL_MOD, ec.fd, &syscall.EpollEvent{
		Events: uint32(syscall.EPOLLOUT | ET_MODE),
		Fd:     int32(ec.fd),
	})
	return
}

func (ec *conn) Close() error {
	// fmt.Println("conn Close")
	syscall.EpollCtl(ec.epfd, syscall.EPOLL_CTL_DEL, int(ec.fd), &syscall.EpollEvent{
		Events: 0,
		Fd:     int32(ec.fd),
	})
	syscall.Close(ec.fd)
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
