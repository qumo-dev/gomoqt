package message

// ZigzagEncode maps a signed integer to an unsigned varint-friendly value
// (0 → 0, -1 → 1, 1 → 2, -2 → 3, ...), as used by the FRAME Timestamp Delta.
func ZigzagEncode(v int64) uint64 {
	return uint64((v << 1) ^ (v >> 63))
}

// ZigzagDecode is the inverse of ZigzagEncode.
func ZigzagDecode(u uint64) int64 {
	return int64(u>>1) ^ -int64(u&1)
}
