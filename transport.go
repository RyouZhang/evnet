package evnet

import (
	"fmt"
	"net"
	"strings"
	"time"

	sys "golang.org/x/sys/unix"
)

const ET_MODE uint32 = 1 << 31

type Transport struct {
	opts Options

	addr *net.TCPAddr

	acceptQueue chan *conn
	shutdown    chan bool

	workers []*runloop
}

func NewTransport(opts Options) *Transport {
	return &Transport{
		opts:        opts,
		acceptQueue: make(chan *conn, 256),
		shutdown:    make(chan bool, 1),
		workers:     make([]*runloop, opts.RunloopNum),
	}
}

func (t *Transport) Listen(network string, addr string) (net.Listener, error) {
	switch strings.ToLower(network) {
	case "tcp":
		{
			var err error
			t.addr, err = net.ResolveTCPAddr(network, addr)
			if err != nil {
				return nil, err
			}

			lfd, epfd, err := t.serve()
			if err != nil {
				return nil, err
			}

			// start listen loop
			go t.listen(lfd, epfd)

			l := newListener(t.addr, t.acceptQueue, t.shutdown)

			for i := 0; i < t.opts.RunloopNum; i++ {
				t.workers[i], err = newRunloop()
				if err != nil {
					sys.Close(epfd)
					sys.Close(lfd)
					return nil, err
				}
				go t.workers[i].mainloop()
			}

			return l, nil
		}
	}
	return nil, fmt.Errorf("unsupport network")
}

func (t *Transport) serve() (int, int, error) {
	fd, err := sys.Socket(sys.AF_INET, sys.O_NONBLOCK|sys.SOCK_STREAM, 0)
	if err != nil {
		return 0, 0, err
	}

	err = sys.Bind(fd, &sys.SockaddrInet4{
		Port: t.addr.Port,
		Addr: [4]byte{t.addr.IP[0], t.addr.IP[1], t.addr.IP[2], t.addr.IP[3]},
	})

	if err != nil {
		sys.Close(fd)
		return 0, 0, err
	}

	err = sys.Listen(fd, 256)
	if err != nil {
		sys.Close(fd)
		return 0, 0, err
	}

	epfd, err := sys.EpollCreate1(sys.EPOLL_CLOEXEC)
	if err != nil {
		sys.Close(fd)
		return 0, 0, err
	}

	sys.EpollCtl(epfd, sys.EPOLL_CTL_ADD, fd, &sys.EpollEvent{
		Events: uint32(sys.EPOLLIN),
		Fd:     int32(fd),
	})
	return fd, epfd, nil
}

func (t *Transport) fetchRunloop() *runloop {
	target := t.workers[0]
	if len(t.workers) == 1 {
		return target
	}
	t.workers = append(t.workers[1:], target)
	return target
}

func (t *Transport) listen(lfd int, epfd int) {
	events := make([]sys.EpollEvent, 8)
	for {
		select {
		case <-t.shutdown:
			{
				if t.opts.GracetAcceptTimeout > 0 {
					<-time.After(time.Duration(t.opts.GracetAcceptTimeout) * time.Second)
				}
				// shudown
				fmt.Println(sys.EpollCtl(epfd, sys.EPOLL_CTL_DEL, lfd, &sys.EpollEvent{
					Events: 0,
					Fd:     int32(lfd),
				}))
				sys.Close(lfd)
				sys.Close(epfd)
				close(t.acceptQueue)

				// grace wait conn
				if t.opts.GraceTimeout > 0 {
					<-time.After(time.Duration(t.opts.GraceTimeout) * time.Second)
				}
				// prepace close runloop
				for _, worker := range t.workers {
					worker.Close()
				}
				return
			}
		default:
			{
				count, err := sys.EpollWait(epfd, events, 1000)
				if err != nil {
					continue
				}
				for i := 0; i < count; i++ {
					event := events[i]

					if int(event.Fd) == lfd {
						fd, sa, err := sys.Accept4(lfd, sys.SOCK_NONBLOCK|sys.SOCK_CLOEXEC)
						if err != nil {
							// do something
							continue
						}

						sys.SetNonblock(fd, true)

						// nodelay
						if t.opts.Nodelay {
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_NODELAY, 1)
						} else {
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_NODELAY, 0)
						}

						// keepalive
						if t.opts.KeepAlive {
							sys.SetsockoptInt(fd, sys.SOL_SOCKET, sys.SO_KEEPALIVE, 1)
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_KEEPIDLE, t.opts.IdleTimeout)
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_KEEPINTVL, t.opts.KeepAliveInterval)
							sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_KEEPCNT, 1)
						} else {
							sys.SetsockoptInt(fd, sys.SOL_SOCKET, sys.SO_KEEPALIVE, 0)
						}

						rl := t.fetchRunloop()

						addr := sa.(*sys.SockaddrInet4)
						c := newConn(fd, t.addr, &net.TCPAddr{
							IP:   []byte{addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3]},
							Port: addr.Port,
						}, rl.msgQueue)

						rl.msgQueue <- &msg{action: actionOpenConn, c: c}
						t.acceptQueue <- c
					}
				}
			}
		}
	}
}
