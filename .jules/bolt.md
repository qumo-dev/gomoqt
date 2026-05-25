## 2025-02-20 - Fast Path varint Decoding
**Learning:** Decoding QUIC varints directly from `io.Reader` using `io.ReadFull` dynamically allocated memory per message. Also calling `ReadVarint` with the stack array caused it to escape to the heap.
**Action:** Use a stack-allocated array (`var buf [8]byte`), implement an `io.ByteReader` fast path to avoid `io.ReadFull` overhead for the first byte, and inline the varint bitwise parsing logic so the array stays on the stack.
