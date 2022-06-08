package evnet

import (
	"fmt"
	"net"
	"strings"
)

const ET_MODE uint32 = 1 << 31

type Transport struct {
	opts Options
	runloopNum int
}

func NewTransport(opts Options) *Transport {
	return &Transport{
		opts:       opts,
		runloopNum: 1,
	}
}

func (t *Transport) RunloopNum(runloopNum int) *Transport {
	t.runloopNum = runloopNum
	return t
}

func (t *Transport) Listen(network string, addr string) (net.Listener, error) {
	switch strings.ToLower(network) {
	case "tcp":
		{
			addrObj, err := net.ResolveTCPAddr(network, addr)
			if err != nil {
				return nil, err
			}

			openQueue := make(chan *conn, 256)
			closeQueue := make(chan *conn, 256)

			l, err := newListener(addrObj, closeQueue, t.opts)
			err = l.serve()
			if err != nil {
				close(openQueue)
				close(closeQueue)
				return nil, err
			}

			workers := make([]*runloop, t.runloopNum)
			for i := 0; i < t.runloopNum; i++ {
				workers[i], err = newRunloop(openQueue, closeQueue)
				if err != nil {
					panic(err)
				}
				go workers[i].mainloop()
			}

			go func() {
				for conn := range l.openQueue {
					openQueue <- conn
				}
				for _, worker := range workers {
					worker.Close()
				}
				close(openQueue)
				close(closeQueue)
			}()
			return l, nil
		}
	}
	return nil, fmt.Errorf("unsupport network")
}
