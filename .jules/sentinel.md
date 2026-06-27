Checking memory constraints for OOM vulnerabilities...
## 2024-06-21 - Fix OOM DoS via unconstrained varint allocation
**Vulnerability:** A memory exhaustion (OOM) vulnerability caused by lack of bounds checking when decoding payloads. The `ReadMessageLength` parsed varint size (up to 2^62-1) was directly used for a `make([]byte, size)` allocation.
**Learning:** An attacker can easily trigger massive memory allocation, crashing the application.
**Prevention:** Apply a size check constraint (e.g. 50MB limit) immediately prior to allocation in `Decode` methods.
## 2024-06-27 - Fix OOM DoS via unconstrained varint allocation in ReadStringArray
**Vulnerability:** A memory exhaustion (OOM) vulnerability caused by lack of bounds checking when pre-allocating string slices in `ReadStringArray`. The parsed varint size was directly used as the capacity for `make([]string, 0, count)`.
**Learning:** Untrusted length prefixes must always be constrained against the remaining buffer length before any pre-allocation, even for slices with a length of 0, because the capacity allocates memory (each element requires at least one byte).
**Prevention:** Check `if count > uint64(len(b))` and clamp it before pre-allocating slices from untrusted byte buffers.
