Checking memory constraints for OOM vulnerabilities...
## 2024-06-21 - Fix OOM DoS via unconstrained varint allocation
**Vulnerability:** A memory exhaustion (OOM) vulnerability caused by lack of bounds checking when decoding payloads. The `ReadMessageLength` parsed varint size (up to 2^62-1) was directly used for a `make([]byte, size)` allocation.
**Learning:** An attacker can easily trigger massive memory allocation, crashing the application.
**Prevention:** Apply a size check constraint (e.g. 50MB limit) immediately prior to allocation in `Decode` methods.
## 2024-06-21 - Fix OOM DoS in ReadStringArray via unconstrained count allocation
**Vulnerability:** A memory exhaustion (OOM) vulnerability caused by lack of bounds checking when parsing string arrays. `ReadStringArray` parsed a varint count and directly allocated `make([]string, 0, count)`.
**Learning:** An attacker can trigger massive slice allocations, causing OOM DoS, by advertising a large element count for an array.
**Prevention:** Ensure parsed array counts cannot exceed the remaining payload length (e.g. `count > len(b)`) before preallocating slices based on untrusted network input.
