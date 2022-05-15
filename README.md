# evnet
a go net.Conn net.Listener implement, base on evio 

# Demo

```
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/RyouZhang/evnet"
)

type MyHandler struct {
}

func (h *MyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(200)
	w.Write([]byte("hello world"))
}

func main() {
	ctx := context.Background()

	ln, err := evnet.NewTransport(evnet.DefaultOptions).Listen("tcp://:7689")
	if err != nil {
		fmt.Println(err)
		return
	}

	srv := &http.Server{Handler: &MyHandler{}}
	srv.SetKeepAlivesEnabled(true)

	go func() {
		fmt.Println(srv.Serve(ln))
	}()

	shutdown := make(chan os.Signal, 1)
	defer close(shutdown)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	<-shutdown
	srv.Shutdown(ctx)
	fmt.Println("finish")
}
```

And you can use it with Gin... so enjoy it.
