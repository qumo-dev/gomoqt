## 2025-06-02 - Eliminate heap allocations in varint parsing
**Learning:** Using `io.ReadFull` with interface parameters causes slices to escape to the heap. For hot paths like reading QUIC varints, `io.ByteReader.ReadByte()` avoids interface-induced heap allocations.
**Action:** When parsing untrusted payload inputs, prefer `io.ByteReader` for single byte reads and map `io.EOF` to `io.ErrUnexpectedEOF` for partial bytes. Avoid `make([]byte)` and `io.ReadFull` inside tight loops whenever possible.
