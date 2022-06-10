package evnet

import (
	"bytes"
	"sync"
)

var bufPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	},
}

func getBuffer() *bytes.Buffer {
	res := bufPool.Get()
	return res.(*bytes.Buffer)
}

func putBuffer(target *bytes.Buffer) {
	target.Reset()
	bufPool.Put(target)
}
