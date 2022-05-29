package evnet

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	// "bytes"
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
	events []syscall.EpollEvent

	sendQueue chan *msg
}

func NewTransport() *Transport {
	return &Transport{
		events:    make([]syscall.EpollEvent, 8),
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
			t.lfd, err = syscall.Socket(syscall.AF_INET, syscall.O_NONBLOCK|syscall.SOCK_STREAM, 0)
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

			err = syscall.Bind(t.lfd, &syscall.SockaddrInet4{
				Port: addrObj.Port,
				Addr: [4]byte{addrObj.IP[0], addrObj.IP[1], addrObj.IP[2], addrObj.IP[3]},
			})
			if err != nil {
				syscall.Close(t.lfd)
				fmt.Println(err)
				return nil, err
			}

			err = syscall.Listen(t.lfd, 256)
			if err != nil {
				syscall.Close(t.lfd)
				fmt.Println(err)
				return nil, err
			}

			t.epfd, err = syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
			if err != nil {
				syscall.Close(t.lfd)
				fmt.Println(err)
				return nil, err
			}

			syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_ADD, t.lfd, &syscall.EpollEvent{
				Events: uint32(syscall.EPOLLIN),
				Fd:     int32(t.lfd),
			})

			t.addr = addrObj

			l, err := newListener(addrObj)

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
		case <-t.l.shutdown:
			{
				fmt.Println("close")
				///shudown
				syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_DEL, t.lfd, &syscall.EpollEvent{
					Events: 0,
					Fd:     int32(t.lfd),
				})
				fmt.Println(syscall.Shutdown(t.lfd, syscall.SHUT_RDWR))
				fmt.Println(syscall.Close(t.lfd))
				// for _, c := range connDic {
				// 	syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_DEL, int(c.fd), &syscall.EpollEvent{
				// 		Events: 0,
				// 		Fd:     int32(c.fd),
				// 	})
				// 	syscall.Close(c.fd)
				// }
				fmt.Println(syscall.Close(t.epfd))
				return
			}
		default:
			{
				count, err := syscall.EpollWait(t.epfd, t.events, 1000)
				if err != nil {
					fmt.Println("Wait", err)
				}
				for i := 0; i < count; i++ {
					event := t.events[i]

					if int(event.Fd) == t.lfd {
						fd, sa, err := syscall.Accept4(t.lfd, syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC)
						if err != nil {
							fmt.Println("accept", err)
							// dosomething
							continue
						}
						syscall.SetNonblock(fd, true)

						syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_DEFER_ACCEPT, 1)
						syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)

						// keepalive
						syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1)
						syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_KEEPIDLE, 300)
						syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_KEEPINTVL, 30)
						syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_KEEPCNT, 1)

						// fmt.Println("accept:", fd)

						addr := sa.(*syscall.SockaddrInet4)
						c := newConn(fd, t.epfd, t.addr, &net.TCPAddr{
							IP:   []byte{addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3]},
							Port: addr.Port,
						})

						syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_ADD, c.fd, &syscall.EpollEvent{
							Events: uint32(syscall.EPOLLIN | ET_MODE),
							Fd:     int32(c.fd),
						})

						connDic[c.fd] = c

						t.l.connQueue <- c
						continue
					}

					c, ok := connDic[int(event.Fd)]
					if !ok {
						syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_DEL, int(event.Fd), &syscall.EpollEvent{
							Events: 0,
							Fd:     int32(event.Fd),
						})
						fmt.Println("invalid fd")
						continue
					}

					if event.Events&(syscall.EPOLLERR) != 0 {
						fmt.Println("EPOLLERR", event.Fd)
						syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_DEL, int(c.fd), &syscall.EpollEvent{
							Events: 0,
							Fd:     int32(c.fd),
						})
						// fmt.Println("remove 1 fd", c.fd)
						// close conn
						delete(connDic, c.fd)
						c.errChan <- fmt.Errorf("bad conn: %d", event.Events)
						continue
					}

					if event.Events&(syscall.EPOLLRDHUP) != 0 {
						fmt.Println("EPOLLRDHUP", event.Fd)
						syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_DEL, int(c.fd), &syscall.EpollEvent{
							Events: 0,
							Fd:     int32(c.fd),
						})
						// fmt.Println("remove 2 fd", c.fd)
						// close conn
						delete(connDic, c.fd)
						c.errChan <- fmt.Errorf("bad conn: %d", event.Events)
						continue
					}

					if event.Events&syscall.EPOLLIN != 0 {

						total := 0
						buf := make([]byte, 4096)
						bufPtr := buf
						for {
							n, err := syscall.Read(c.fd, bufPtr)
							if n > 0 {
								total = total + n
								bufPtr = bufPtr[n:]
								continue
							}
							if n == 0 {
								goto Next
							}
							if n == -1 {
								switch {
								case errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR):
									goto Next
								default:
									{
										syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_DEL, int(c.fd), &syscall.EpollEvent{
											Events: 0,
											Fd:     int32(c.fd),
										})
										delete(connDic, c.fd)
										c.errChan <- err
										goto End
									}
								}
							}
						}
					Next:
						if total > 0 {
							if total == len(buf) {
								c.input <- buf
							} else {
								c.input <- buf[:total]
							}
						}

						if c.outBuf.Len() > 0 {
							syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_MOD, c.fd, &syscall.EpollEvent{
								Events: uint32(syscall.EPOLLOUT | ET_MODE),
								Fd:     int32(c.fd),
							})
						} else {
							syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_MOD, c.fd, &syscall.EpollEvent{
								Events: uint32(syscall.EPOLLIN | ET_MODE),
								Fd:     int32(c.fd),
							})
						}
					End:
						continue
					}

					if event.Events&syscall.EPOLLOUT != 0 {
						// fmt.Println("EPOLLOUT", event.Fd)
						select {
						case raw := <-c.output:
							{
								c.outBuf.Write(raw)
							}
						default:
							{
							}
						}

						if c.outBuf.Len() > 0 {
							buf := make([]byte, 4096)

							n, _ := c.outBuf.Read(buf)
							_, err := syscall.Write(c.fd, buf[:n])
							if err != nil {
								fmt.Println("EPOLLOUT", c.fd, err)
								syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_DEL, int(c.fd), &syscall.EpollEvent{
									Events: 0,
									Fd:     int32(c.fd),
								})
								delete(connDic, c.fd)
								c.errChan <- err
								goto B
							}
						}

						if c.outBuf.Len() == 0 {
							c.outBuf.Reset()
						}

						if c.outBuf.Len() > 0 {
							syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_MOD, c.fd, &syscall.EpollEvent{
								Events: uint32(syscall.EPOLLOUT | ET_MODE),
								Fd:     int32(c.fd),
							})
						} else {
							syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_MOD, c.fd, &syscall.EpollEvent{
								Events: uint32(syscall.EPOLLIN | ET_MODE),
								Fd:     int32(c.fd),
							})
						}
					B:
						continue

						// temp := make([]byte, 4096)

						// m, err := c.outBuf.Read(temp)
						// if err != nil {
						// 	fmt.Println("EPOLLOUT", c.fd, m, err)
						// 	continue
						// }
						// m, err = syscall.Write(c.fd, temp[:m])
						// if err != nil {
						// 	fmt.Println("EPOLLOUT", c.fd, err)

						// 	fmt.Println("add 3")
						// 	syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_MOD, c.fd, &syscall.EpollEvent{
						// 		Events: uint32(syscall.EPOLLIN | ET_MODE),
						// 		Fd:     int32(c.fd),
						// 	})
						// 	continue
						// }

						// if c.outBuf.Len() > 0 {
						// 	// fmt.Println("add 1")
						// 	syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_MOD, c.fd, &syscall.EpollEvent{
						// 		Events: uint32(syscall.EPOLLOUT | ET_MODE),
						// 		Fd:     int32(c.fd),
						// 	})
						// } else {
						// 	syscall.EpollCtl(t.epfd, syscall.EPOLL_CTL_MOD, c.fd, &syscall.EpollEvent{
						// 		Events: uint32(syscall.EPOLLIN | ET_MODE),
						// 		Fd:     int32(c.fd),
						// 	})
						// }
					}
				}
			}
		}
	}
}
