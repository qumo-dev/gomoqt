## 2025-06-01 - Allocations in io.Reader wrappers
**Learning:** Passing slice buffers like `buf[:]` to `io.ReadFull` or `io.Reader.Read` escapes the slice to the heap, causing 2 allocations per call (one for the slice header, one for the array). This is highly detrimental in a tight loop reading varints (like `ReadMessageLength`).
**Action:** Always check if the `io.Reader` implements `io.ByteReader`. Using `br.ReadByte()` eliminates the slice escape analysis and brings allocations to zero, significantly boosting throughput.
