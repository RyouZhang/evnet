package evnet

import (
	"net"
	"time"

	"github.com/tidwall/evio"
)

type Options struct {
	TCPKeepAlive     time.Duration
	ReuseInputBuffer bool
	NumLoops         int
	BufferSize       int
}

var DefaultOptions = Options{
	TCPKeepAlive:     300 * time.Second,
	ReuseInputBuffer: true,
	NumLoops:         1,
	BufferSize:       4096,
}

type Transport struct {
	opts Options

	info   evio.Server
	events evio.Events

	l        *listener
	starting chan bool
}

func NewTransport(opts Options) *Transport {
	srv := &Transport{
		starting: make(chan bool),
		opts:     opts,
	}

	srv.events.NumLoops = srv.opts.NumLoops
	srv.events.LoadBalance = evio.RoundRobin
	srv.events.Serving = srv.onServing
	srv.events.Tick = srv.onTick
	srv.events.Opened = srv.onOpened
	srv.events.Closed = srv.onClosed
	srv.events.Data = srv.onData

	return srv
}

func (s *Transport) onServing(info evio.Server) (act evio.Action) {
	s.info = info
	// create listener
	close(s.starting)
	return
}

func (s *Transport) onTick() (delay time.Duration, act evio.Action) {
	delay = time.Duration(1 * time.Second)
	select {
	case <-s.l.shutdown:
		{
			//process shutdown
			act = evio.Shutdown
			close(s.l.connQueue)
		}
	default:
		{
			//do nothing
		}
	}
	return
}

func (s *Transport) onOpened(c evio.Conn) (out []byte, opts evio.Options, act evio.Action) {
	s.l.connQueue <- newEvConn(c, s.opts.BufferSize)
	opts = evio.Options{
		TCPKeepAlive:     s.opts.TCPKeepAlive,
		ReuseInputBuffer: s.opts.ReuseInputBuffer,
	}
	return
}

func (s *Transport) onClosed(c evio.Conn, err error) (act evio.Action) {
	ec := c.Context().(*conn)
	if err != nil {
		ec.errChan <- err
	}
	close(ec.errChan)
	close(ec.recvQueue)
	return
}

func (s *Transport) onData(c evio.Conn, in []byte) (out []byte, act evio.Action) {
	ec := c.Context().(*conn)
	if in != nil {
		// recv
		ec.recvQueue <- in
	} else {
		// send
		select {
		case out = <-ec.sendQueue:
			{
			}
		case <-ec.shutdown:
			{
				act = evio.Close
			}
		default:
			{
			}
		}
	}
	return
}

func (s *Transport) Listen(addr string) (net.Listener, error) {
	var err error

	go func() {
		err = evio.Serve(s.events, addr)
	}()
	<-s.starting
	if err != nil {
		return nil, err
	}

	s.l = newListener(s.info)

	return s.l, nil
}
