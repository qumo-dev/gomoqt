## 2024-06-10 - DoS via Memory Allocation Panics
**Vulnerability:** The `message_reader.go` and `frame.go` components panicked or allocated unbounded memory when encountering malicious varints defining massive payload sizes.
**Learning:** Parsing network data length from varints directly into `make([]byte, size)` without sensible upper boundaries or replacing errors with `panic()` constructs creates an exploitable DoS condition.
**Prevention:** Always bound byte allocations from network buffers, and return standard errors (e.g. `errors.New()`, `io.ErrUnexpectedEOF`) rather than panicking on out-of-bounds size requests.
