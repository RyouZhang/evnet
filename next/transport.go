package next

import (
	"fmt"
	"net"
	"time"

	gnet "github.com/panjf2000/gnet/v2"
)

type Transport struct {
	eng      gnet.Engine
	l        *listener
	starting chan bool
}

func NewTransport() *Transport {
	return &Transport{
		starting: make(chan bool),
	}
}

func (t *Transport) parseAddr(network, address string) (net.Addr, error) {
	tcpAddr, err := net.ResolveTCPAddr(network, address)
	if err == nil {
		return tcpAddr, nil
	}
	udpAddr, err := net.ResolveUDPAddr(network, address)
	if err == nil {
		return udpAddr, nil
	}
	ipAddr, err := net.ResolveIPAddr(network, address)
	if err == nil {
		return ipAddr, nil
	}
	unixAddr, err := net.ResolveUnixAddr(network, address)
	if err == nil {
		return unixAddr, nil
	}
	return nil, fmt.Errorf("invalid network or address")
}

func (t *Transport) Listen(network, address string) (*listener, error) {
	addr, err := t.parseAddr(network, address)
	if err != nil {
		return nil, err
	}
	go func() {
		err = gnet.Run(t, fmt.Sprintf("%s://%s", addr.Network(), addr.String()), gnet.WithTicker(true))
	}()
	<-t.starting
	if err != nil {
		return nil, err
	}

	t.l = newListener(addr)

	return t.l, nil
}

func (t *Transport) OnBoot(eng gnet.Engine) (action gnet.Action) {
	t.eng = eng
	close(t.starting)
	return
}

func (t *Transport) OnShutdown(eng gnet.Engine) {
	// do nothing
	fmt.Println(t.eng.CountConnections())
}

func (t *Transport) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	t.l.connQueue <- newEvConn(c)
	return
}

func (t *Transport) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	// ec, ok := c.Context().(*conn)
	// if ok {
	// 	if err != nil {
	// 		ec.errChan <- err
	// 	}
	// 	close(ec.errChan)
	// }
	return
}

func (t *Transport) OnTraffic(c gnet.Conn) (action gnet.Action) {
	// fo nothing
	// ec := c.Context().(*conn)
	// if c.InboundBuffered() > 0 {
	// 	buf := make([]byte, c.InboundBuffered())
	// 	// recv
	// 	c.Read(buf)
	// 	ec.recvQueue <- buf
	// }
	return
}

func (t *Transport) OnTick() (delay time.Duration, action gnet.Action) {
	delay = time.Duration(1 * time.Second)
	if t.l == nil {
		return
	}
	select {
	case <-t.l.shutdown:
		{
			//process shutdown
			action = gnet.Shutdown
			close(t.l.connQueue)
		}
	default:
		{
			//do nothing
		}
	}
	return
}
