package message

import "sync"

var bufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 1024)
		return &b
	},
}

func getBuf() *[]byte {
	return bufPool.Get().(*[]byte)
}

func putBuf(b *[]byte) {
	// Need to clear it this way to avoid the self-assignment issue
	tmp := (*b)[:0]
	*b = tmp
	bufPool.Put(b)
}
