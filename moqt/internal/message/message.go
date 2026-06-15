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
