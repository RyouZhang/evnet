package evnet

import (
	"fmt"
	"io"
	"net"
	"sync"

	sys "golang.org/x/sys/unix"
)

type listener struct {
	opts Options
	addr *net.TCPAddr

	shutdown   chan bool
	cOnce      sync.Once
	connQueue  chan *conn
	openQueue  chan *conn
	closeQueue chan *conn
}

func newListener(addr *net.TCPAddr, closeQueue chan *conn, opts Options) (*listener, error) {
	return &listener{
		opts:       opts,
		addr:       addr,
		shutdown:   make(chan bool, 1),
		connQueue:  make(chan *conn, 256),
		openQueue:  make(chan *conn, 256),
		closeQueue: closeQueue,
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
	l.cOnce.Do(func() {
		close(l.shutdown)
	})
	return nil
}

func (l *listener) Addr() net.Addr {
	return l.addr
}

func (l *listener) serve() error {
	fd, err := sys.Socket(sys.AF_INET, sys.O_NONBLOCK|sys.SOCK_STREAM, 0)
	if err != nil {
		return err
	}

	err = sys.Bind(fd, &sys.SockaddrInet4{
		Port: l.addr.Port,
		Addr: [4]byte{l.addr.IP[0], l.addr.IP[1], l.addr.IP[2], l.addr.IP[3]},
	})

	if err != nil {
		sys.Close(fd)
		return err
	}

	err = sys.Listen(fd, 256)
	if err != nil {
		sys.Close(fd)
		return err
	}

	epfd, err := sys.EpollCreate1(sys.EPOLL_CLOEXEC)
	if err != nil {
		sys.Close(fd)
		return err
	}

	sys.EpollCtl(epfd, sys.EPOLL_CTL_ADD, fd, &sys.EpollEvent{
		Events: uint32(sys.EPOLLIN),
		Fd:     int32(fd),
	})

	go l.mainloop(fd, epfd)
	return nil
}

func (l *listener) mainloop(lfd int, epfd int) {
	events := make([]sys.EpollEvent, 8)
	for {
		select {
		case <-l.shutdown:
			{
				// shudown
				fmt.Println(sys.EpollCtl(epfd, sys.EPOLL_CTL_DEL, lfd, &sys.EpollEvent{
					Events: 0,
					Fd:     int32(lfd),
				}))
				sys.Close(lfd)
				close(l.connQueue)
				close(l.openQueue)
				return
			}
		default:
			{
				count, err := sys.EpollWait(epfd, events, 1000)
				if err != nil {
					fmt.Println(err)
					continue
				}
				for i := 0; i < count; i++ {
					event := events[i]

					if int(event.Fd) == lfd {
						fd, sa, err := sys.Accept4(lfd, sys.SOCK_NONBLOCK|sys.SOCK_CLOEXEC)
						if err != nil {
							fmt.Println("accept", err)
							// dosomething
							continue
						}
						fmt.Println("Accept:", fd)

						sys.SetNonblock(fd, true)

						// nodelay
						if l.opts.Nodelay {
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_NODELAY, 1)
						} else {
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_NODELAY, 0)
						}

						// keepalive
						if l.opts.KeepAlive {
							sys.SetsockoptInt(fd, sys.SOL_SOCKET, sys.SO_KEEPALIVE, 1)
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_KEEPIDLE, l.opts.IdleTimeout)
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_KEEPINTVL, l.opts.KeepAliveInterval)
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_KEEPCNT, 1)
						} else {
							sys.SetsockoptInt(fd, sys.SOL_SOCKET, sys.SO_KEEPALIVE, 0)
						}

						//read write timeout
						// sys.SetsockoptTimeval(fd, sys.SOL_SOCKET, sys.SO_RCVTIMEO, &sys.Timeval{Sec: 1})
						// sys.SetsockoptTimeval(fd, sys.SOL_SOCKET, sys.SO_SNDTIMEO, &sys.Timeval{Sec: 1})

						addr := sa.(*sys.SockaddrInet4)
						c := newConn(fd, l.addr, &net.TCPAddr{
							IP:   []byte{addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3]},
							Port: addr.Port,
						}, l.closeQueue)

						l.openQueue <- c
						l.connQueue <- c
						continue
					}
				}
			}
		}
	}
}
