Checking memory constraints for OOM vulnerabilities...
## 2024-06-21 - Fix OOM DoS via unconstrained varint allocation
**Vulnerability:** A memory exhaustion (OOM) vulnerability caused by lack of bounds checking when decoding payloads. The `ReadMessageLength` parsed varint size (up to 2^62-1) was directly used for a `make([]byte, size)` allocation.
**Learning:** An attacker can easily trigger massive memory allocation, crashing the application.
**Prevention:** Apply a size check constraint (e.g. 50MB limit) immediately prior to allocation in `Decode` methods.
## 2024-06-29 - Fix OOM DoS via unconstrained varint allocation in ReadStringArray
**Vulnerability:** A memory exhaustion (OOM) vulnerability caused by lack of bounds checking when decoding string arrays. The `ReadVarint` parsed array count was directly used for a `make([]string, 0, count)` allocation.
**Learning:** An attacker can easily trigger massive memory allocation using a small payload with a huge array count prefix, crashing the application.
**Prevention:** Verify that the requested `count` does not exceed the remaining buffer length (`count > uint64(len(b))`) before preallocating the slice.
