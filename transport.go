package evnet

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"
	// "syscall"

	sys "golang.org/x/sys/unix"
)

type msg struct {
	fd  int
	raw []byte
}

const ET_MODE uint32 = 1 << 31

type Transport struct {
	lfd    int
	epfd   int
	addr   net.Addr
	l      *listener
	events []sys.EpollEvent

	connQueue chan *conn
	closeQueue chan int

	sendQueue chan *msg
}

func NewTransport() *Transport {
	return &Transport{
		events:    make([]sys.EpollEvent, 8),
		connQueue: make(chan *conn, 256),
		closeQueue: make(chan int, 64),
		sendQueue: make(chan *msg, 1024),
	}
}

func (t *Transport) Listen(network string, addr string) (net.Listener, error) {
	switch strings.ToLower(network) {
	case "tcp":
		{
			addrObj, err := net.ResolveTCPAddr(network, addr)
			if err != nil {
				fmt.Println(err)
				return nil, err
			}

			// if string(addrObj.IP.To4()) == string(addrObj.IP) {
			t.lfd, err = sys.Socket(sys.AF_INET, sys.O_NONBLOCK|sys.SOCK_STREAM, 0)
			if err != nil {
				fmt.Println(err)
				return nil, err
			}
			// err = syscall.SetsockoptInt(t.lfd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 0)
			// if err != nil {
			// 	syscall.Close(t.lfd)
			// 	fmt.Println(err)
			// 	return nil, err
			// }

			err = sys.Bind(t.lfd, &sys.SockaddrInet4{
				Port: addrObj.Port,
				Addr: [4]byte{addrObj.IP[0], addrObj.IP[1], addrObj.IP[2], addrObj.IP[3]},
			})
			if err != nil {
				sys.Close(t.lfd)
				fmt.Println(err)
				return nil, err
			}

			err = sys.Listen(t.lfd, 256)
			if err != nil {
				sys.Close(t.lfd)
				fmt.Println(err)
				return nil, err
			}

			t.epfd, err = sys.EpollCreate1(sys.EPOLL_CLOEXEC)
			if err != nil {
				sys.Close(t.lfd)
				fmt.Println(err)
				return nil, err
			}

			sys.EpollCtl(t.epfd, sys.EPOLL_CTL_ADD, t.lfd, &sys.EpollEvent{
				Events: uint32(sys.EPOLLIN),
				Fd:     int32(t.lfd),
			})

			t.addr = addrObj

			l, err := newListener(addrObj, t.connQueue)

			t.l = l

			go t.runloop()
			fmt.Println("server start")
			return l, err
			// }
		}
	}
	// case "udp": {
	// 	addrObj, err := net.ResolveUDPAddr(network, addr)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// }
	// case "unix": {
	// 	addrObj, err := net.ResolveUnixAddr(network, addr)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// }
	// }
	return nil, fmt.Errorf("unsupport network")
}

func (t *Transport) runloop() {
	connDic := make(map[int]*conn)
	for {
		select {
		case fd := <-t.closeQueue:
			{
				_, ok := connDic[fd]
				if ok {
					sys.EpollCtl(t.epfd, sys.EPOLL_CTL_DEL, int(fd), &sys.EpollEvent{
						Events: 0,
						Fd:     int32(fd),
					})
					sys.Close(fd)
					delete(connDic, fd)
				}
			}
		case <-t.l.shutdown:
			{
				///shudown
				fmt.Println(sys.EpollCtl(t.epfd, sys.EPOLL_CTL_DEL, t.lfd, &sys.EpollEvent{
					Events: 0,
					Fd:     int32(t.lfd),
				}))
				// fmt.Println(syscall.Shutdown(t.lfd, syscall.SHUT_RDWR))
				fmt.Println(sys.Close(t.lfd))
				fmt.Println(connDic)
				for _, c := range connDic {
					// sys.EpollCtl(t.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
					// 	Events: 0,
					// 	Fd:     int32(c.fd),
					// })
					// sys.Close(c.fd)
					c.errChan <- fmt.Errorf("bad conn: %d",c.fd)
					close(c.errChan)
				}
				// fmt.Println(sys.Close(t.epfd))
				fmt.Println("closed")
				// close(t.connQueue)
				// close(t.closeQueue)
				return
			}
		default:
			{
				count, err := sys.EpollWait(t.epfd, t.events, 1000)
				if err != nil {
					continue
				}
				for i := 0; i < count; i++ {
					event := t.events[i]

					if int(event.Fd) == t.lfd {
						fd, sa, err := sys.Accept4(t.lfd, sys.SOCK_NONBLOCK|sys.SOCK_CLOEXEC)
						if err != nil {
							fmt.Println("accept", err)
							// dosomething
							continue
						}

						fmt.Println("acccept", fd)
						
						sys.SetNonblock(fd, true)

						// nodelay
						sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_NODELAY, 1)

						// keepalive
						sys.SetsockoptInt(fd, sys.SOL_SOCKET, sys.SO_KEEPALIVE, 1)
						sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_KEEPIDLE, 300)
						sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_KEEPINTVL, 30)
						sys.SetsockoptInt(fd, sys.IPPROTO_TCP, sys.TCP_KEEPCNT, 1)

						//read write timeout
						sys.SetsockoptTimeval(fd, sys.SOL_SOCKET, sys.SO_RCVTIMEO, &sys.Timeval{Sec:1})
						sys.SetsockoptTimeval(fd, sys.SOL_SOCKET, sys.SO_SNDTIMEO, &sys.Timeval{Sec:1})

						addr := sa.(*sys.SockaddrInet4)
						c := newConn(fd, t.closeQueue, t.addr, &net.TCPAddr{
							IP:   []byte{addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3]},
							Port: addr.Port,
						})

						sys.EpollCtl(t.epfd, sys.EPOLL_CTL_ADD, c.fd, &sys.EpollEvent{
							Events: uint32(sys.EPOLLIN  | ET_MODE | sys.EPOLLERR | sys.EPOLLRDHUP),
							Fd:     int32(c.fd),
						})

						connDic[c.fd] = c

						t.connQueue <- c
						continue
					}

					c, ok := connDic[int(event.Fd)]
					if !ok {
						sys.EpollCtl(t.epfd, sys.EPOLL_CTL_DEL, int(event.Fd), &sys.EpollEvent{
							Events: 0,
							Fd:     int32(event.Fd),
						})
						fmt.Println("invalid fd")
						sys.Close(c.fd)
						continue
					}

					if event.Events&(sys.EPOLLERR) != 0 {
						fmt.Println("EPOLLERR", event.Fd)
						sys.EpollCtl(t.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
							Events: 0,
							Fd:     int32(c.fd),
						})

						sys.Close(c.fd)
						delete(connDic, c.fd)
						c.errChan <- fmt.Errorf("bad conn: %d", event.Events)
						close(c.errChan)
						continue
					}

					if event.Events&(sys.EPOLLRDHUP) != 0 {
						fmt.Println("EPOLLRDHUP", event.Fd)
						sys.EpollCtl(t.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
							Events: 0,
							Fd:     int32(c.fd),
						})
						sys.Close(c.fd)
						delete(connDic, c.fd)
						c.errChan <- fmt.Errorf("bad conn: %d", event.Events)
						close(c.errChan)
						continue
					}

					if event.Events&sys.EPOLLIN != 0 {
						// fmt.Println("EPOLLIN", event.Fd)
						buf := bytes.NewBuffer(make([]byte, 0, 4096))
						temp := make([]byte, 4096)
						for {
							n, err := sys.Read(c.fd, temp)
							if n > 0 {
								buf.Write(temp[:n])
								continue
							}
							if n == 0 {
								goto Next
							}
							if n == -1 {
								switch {
								case errors.Is(err, sys.EAGAIN) || errors.Is(err, sys.EINTR):
									goto Next
								default:
									{
										fmt.Println("EPOLLIN", c.fd, err)
										sys.EpollCtl(t.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
											Events: 0,
											Fd:     int32(c.fd),
										})
										sys.Close(c.fd)
										delete(connDic, c.fd)
										c.errChan <- err
										goto End
									}
								}
							}
						}
					Next:
						if buf.Len() > 0 {
							c.input <- buf.Bytes()
						}
					End:
						continue
					}

					// if event.Events&sys.EPOLLOUT != 0 {
					// 	fmt.Println("out", c.fd)
					// 	continue
					// }
					// 	c.outMux.Lock()

					// 	// fmt.Println("EPOLLOUT", event.Fd)
					// 	if c.outBuf.Len() > 0 {
					// 		buf := make([]byte, 4096)

					// 		n, _ := c.outBuf.Read(buf)
					// 		_, err := syscall.Write(c.fd, buf[:n])
					// 		if err != nil {
					// 			fmt.Println("EPOLLOUT", c.fd, err)
					// 			syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_DEL, int(c.fd), &syscall.EpollEvent{
					// 				Events: 0,
					// 				Fd:     int32(c.fd),
					// 			})
					// 			syscall.Close(c.fd)
					// 			delete(connDic, c.fd)
					// 			c.errChan <- err
					// 			c.outMux.Unlock()
					// 			continue
					// 		}
					// 	}

					// 	if c.outBuf.Len() == 0 {
					// 		c.outBuf.Reset()
					// 		syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_MOD, c.fd, &syscall.EpollEvent{
					// 			Events: uint32(syscall.EPOLLIN | ET_MODE | syscall.EPOLLERR | syscall.EPOLLRDHUP),
					// 			Fd:     int32(c.fd),
					// 		})
					// 	}
					// 	c.outMux.Unlock()
					// 	continue
					// }
				}
			}
		}
	}
}
