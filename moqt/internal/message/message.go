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
		l := len(s)
		total += l
		if uint64(l) <= maxVarInt1 {
			total += 1
		} else if uint64(l) <= maxVarInt2 {
			total += 2
		} else if uint64(l) <= maxVarInt4 {
			total += 4
		} else {
			total += 8
		}
	}
	return total
}

// MaxMessageSize is the maximum size of a message payload in bytes (50MB).
// This limit prevents out-of-memory (OOM) denial-of-service attacks
// when reading maliciously crafted message length prefixes.
const MaxMessageSize = 50 * 1024 * 1024
