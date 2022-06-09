package evnet

type Options struct {
	Nodelay             bool
	KeepAlive           bool
	KeepAliveInterval   int //sec
	IdleTimeout         int //sec
	GracetAcceptTimeout int //sec
	GraceTimeout        int //sec
	RunloopNum          int
}

func DefaultOptions() Options {
	return Options{
		Nodelay:             true,
		KeepAlive:           true,
		KeepAliveInterval:   30,
		IdleTimeout:         300,
		RunloopNum:          1,
		GracetAcceptTimeout: 0,
		GraceTimeout:        0,
	}
}
