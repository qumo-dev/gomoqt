## 2024-06-06 - io.Reader allocations with stack arrays
**Learning:** Passing a slice of a stack-allocated array (e.g., `buf[:]`) to `io.Reader.Read` or `io.ReadFull` causes the array to escape to the heap, resulting in unintended memory allocations.
**Action:** Use an `io.ByteReader` fast path to avoid these allocations completely for small, byte-by-byte reads.
