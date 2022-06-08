package evnet

type Options struct {
	Nodelay           bool
	KeepAlive         bool
	KeepAliveInterval int //sec
	IdleTimeout       int //sec
}

func DefaultOptions() Options {
	return Options{
		Nodelay:           true,
		KeepAlive:         true,
		KeepAliveInterval: 30,
		IdleTimeout:       300,
	}
}
