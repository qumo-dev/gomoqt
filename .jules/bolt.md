## 2025-02-23 - Avoid io.Reader heap allocations in hot paths
**Learning:** Using `make([]byte, 1)` and then `io.ReadFull(r, buf)` triggers escape analysis and allocates memory on the heap. Small slice allocations add up quickly in parsers like varint or byte readers for protocols.
**Action:** Use type assertion `if br, ok := r.(io.ByteReader)` to process single byte reads without slice allocations. If slices must be used, use fixed-size stack arrays `var buf [8]byte` and slice them instead of `make([]byte)`.
