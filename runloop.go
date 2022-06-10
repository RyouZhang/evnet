package evnet

import (
	"errors"
	"fmt"
	"sync"

	sys "golang.org/x/sys/unix"
)

const actionOpenConn = 1
const actionCloseConn = 2
const actionShutdown = 3

type msg struct {
	action int // 1 add 2 del 3 shutdown
	c      *conn
}

type runloop struct {
	epfd int

	wg       sync.WaitGroup
	msgQueue chan *msg
}

func newRunloop() (*runloop, error) {
	epfd, err := sys.EpollCreate1(sys.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return &runloop{
		epfd:     epfd,
		msgQueue: make(chan *msg, 512),
	}, nil
}

func (r *runloop) mainloop() {
	events := make([]sys.EpollEvent, 8)
	connDic := make(map[int]*conn)
	temp := make([]byte, 4096)
	isShutdown := false
	for {
		select {
		case m, ok := <-r.msgQueue:
			{
				if ok {
					switch m.action {
					case actionOpenConn:
						{
							r.wg.Add(1)
							c := m.c
							connDic[c.fd] = c

							sys.EpollCtl(r.epfd, sys.EPOLL_CTL_ADD, c.fd, &sys.EpollEvent{
								Events: uint32(sys.EPOLLIN | ET_MODE | sys.EPOLLRDHUP | sys.EPOLLHUP),
								Fd:     int32(c.fd),
							})
						}
					case actionCloseConn:
						{
							c := m.c
							_, ok := connDic[c.fd]
							if ok {
								delete(connDic, c.fd)
								sys.EpollCtl(r.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
									Events: 0,
									Fd:     int32(c.fd),
								})
								sys.Close(c.fd)
								r.wg.Done()
							}
						}
					case actionShutdown:
						{
							isShutdown = true
							for _, c := range connDic {
								c.errChan <- fmt.Errorf("broke fd: %d", c.fd)
							}
						}
					}
				}

				if isShutdown && len(connDic) == 0 {
					sys.Close(r.epfd)
					return
				}
			}
		default:
			{
				count, err := sys.EpollWait(r.epfd, events, 1000)
				if err != nil {	
					continue
				}
				for i := 0; i < count; i++ {
					event := events[i]

					c, ok := connDic[int(event.Fd)]
					if !ok {
						sys.EpollCtl(r.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
							Events: 0,
							Fd:     int32(c.fd),
						})
						sys.Close(c.fd)
						continue
					}

					if event.Events&sys.EPOLLHUP != 0 {
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
						// for half we only close epollin
						sys.EpollCtl(r.epfd, sys.EPOLL_CTL_DEL, int(c.fd), &sys.EpollEvent{
							Events: uint32(sys.EPOLLIN | sys.EPOLLRDHUP),
							Fd:     int32(c.fd),
						})
					}

					if event.Events&sys.EPOLLIN != 0 {
						buf := getBuffer()
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
						// do nothing
					}
				}
			}
		}
	}
}

func (r *runloop) Close() {
	r.msgQueue <- &msg{action: actionShutdown}
	r.wg.Wait()
	close(r.msgQueue)
}
