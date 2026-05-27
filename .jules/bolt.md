## 2024-05-27 - Escape analysis with local slices in `io.ReadFull`
**Learning:** When reading small amounts of bytes from an `io.Reader` in Go, passing a slice of a stack-allocated array (e.g., `buf[:]`) to interface methods like `io.Reader.Read` or `io.ReadFull` causes the array to escape to the heap due to escape analysis.
**Action:** To avoid allocation overhead, type-assert the reader to `io.ByteReader` and read bytes individually. Fallback to `io.ReadFull` if type assertion fails.
