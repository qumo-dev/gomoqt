## 2024-05-24 - io.Reader allocations in hot path
**Learning:** `ReadMessageLength` was allocating slices on the heap (`make([]byte, size)`) for reading varints, which is passed to an `io.Reader`, causing the slices to escape to the heap.
**Action:** Use a fixed-size stack-allocated array (`var buf [8]byte`) and use type assertion for `io.ByteReader` to bypass `io.ReadFull` when reading a single byte.
