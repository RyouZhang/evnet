package evnet

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	sys "golang.org/x/sys/unix"
)

type runloop struct {
	epfd   int
	events []sys.EpollEvent

	wg         sync.WaitGroup
	openQueue  chan *conn
	closeQueue chan *conn
	shutdown   chan bool
}

func newRunloop(openQueue chan *conn, closeQueue chan *conn) (*runloop, error) {
	epfd, err := sys.EpollCreate1(sys.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return &runloop{
		epfd:       epfd,
		events:     make([]sys.EpollEvent, 8),
		shutdown:   make(chan bool, 1),
		openQueue:  openQueue,
		closeQueue: closeQueue,
	}, nil
}

func (r *runloop) mainloop() {
	connDic := make(map[int]*conn)
	for {
		select {
		case <-r.shutdown:
			{
				fmt.Println(connDic)
				for _, c := range connDic {
					c.errChan <- fmt.Errorf("close fd: %d", c.fd)
				}
				if len(connDic) == 0 {
					return
				}
			}
		case c := <-r.closeQueue:
			{
				fmt.Println("closeing:", c)
				_, ok := connDic[c.fd]
				if ok {
					delete(connDic, c.fd)

					sys.EpollCtl(r.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
						Events: 0,
						Fd:     int32(c.fd),
					})
					sys.Close(c.fd)

					r.wg.Done()
					fmt.Println(connDic)
				}
			}
		case c := <-r.openQueue:
			{
				r.wg.Add(1)
				connDic[c.fd] = c
				fmt.Println("open:", c)
				c.closeQueue = r.closeQueue

				sys.EpollCtl(r.epfd, sys.EPOLL_CTL_ADD, c.fd, &sys.EpollEvent{
					Events: uint32(sys.EPOLLIN | ET_MODE | sys.EPOLLRDHUP | sys.EPOLLHUP),
					Fd:     int32(c.fd),
				})
			}
		default:
			{
				count, err := sys.EpollWait(r.epfd, r.events, 1000)
				if err != nil {
					fmt.Println(err)
					continue
				}
				for i := 0; i < count; i++ {
					event := r.events[i]

					c, ok := connDic[int(event.Fd)]
					if !ok {
						sys.EpollCtl(r.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
							Events: 0,
							Fd:     int32(c.fd),
						})
						sys.Close(c.fd)
						fmt.Println("invalid fd")
						continue
					}
					// fmt.Println(c.fd, event.Events&sys.EPOLLIN, event.Events&sys.EPOLLRDHUP, event.Events & sys.EPOLLOUT)

					if event.Events&sys.EPOLLHUP != 0 {
						fmt.Println("EPOLLHUP", event.Fd)
						delete(connDic, c.fd)

						sys.EpollCtl(r.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
							Events: 0,
							Fd:     int32(c.fd),
						})
						sys.Close(c.fd)

						r.wg.Done()
						c.errChan <- fmt.Errorf("broke fd: %d", c.fd)
						continue
					}

					if event.Events&sys.EPOLLRDHUP != 0 {
						fmt.Println("EPOLLRDHUP", event.Fd)
						// for half we only close epollin
						sys.EpollCtl(r.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
							Events: uint32(sys.EPOLLIN | sys.EPOLLRDHUP),
							Fd:     int32(c.fd),
						})
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
										delete(connDic, c.fd)

										sys.EpollCtl(r.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
											Events: 0,
											Fd:     int32(c.fd),
										})
										sys.Close(c.fd)

										r.wg.Done()
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
				}
			}
		}
	}
}

func (r *runloop) Close() {
	r.shutdown <- true
	r.wg.Wait()
	close(r.shutdown)
	sys.Close(r.epfd)
}
