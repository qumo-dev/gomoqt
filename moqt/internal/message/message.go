package message

import (
	"fmt"
)

func VarintLen(i uint64) int {
	if i <= maxVarInt1 {
		return 1
	}
	if i <= maxVarInt2 {
		return 2
	}
	if i <= maxVarInt4 {
		return 4
	}
	if i <= maxVarInt8 {
		return 8
	}
	panic(fmt.Sprintf("%#x doesn't fit into 62 bits", i))
}

func StringLen(s string) int {
	return VarintLen(uint64(len(s))) + len(s)
}

func BytesLen(b []byte) int {
	return VarintLen(uint64(len(b))) + len(b)
}

func StringArrayLen(arr []string) int {
	total := VarintLen(uint64(len(arr)))
	for _, s := range arr {
		total += StringLen(s)
	}
	return total
}

// MaxMessageSize is the largest control message body a decoder accepts
// (64 KiB). Decoders allocate the declared body before reading it, so this
// bounds what a peer can make this endpoint reserve per stream without
// sending the bytes, including a native-QUIC SETUP read before any
// application auth. No control message comes close: the largest, a SETUP
// whose Path carries a credential in its query, is a few KiB.
const MaxMessageSize = 64 * 1024

// MaxFrameSize is the largest frame payload accepted (50 MiB). Unlike a
// control message, a frame is read into a buffer that grows as its bytes
// arrive, so a declared length alone reserves nothing.
const MaxFrameSize = 50 * 1024 * 1024
