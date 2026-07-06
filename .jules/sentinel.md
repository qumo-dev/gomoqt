Checking memory constraints for OOM vulnerabilities...
## 2024-06-21 - Fix OOM DoS via unconstrained varint allocation
**Vulnerability:** A memory exhaustion (OOM) vulnerability caused by lack of bounds checking when decoding payloads. The `ReadMessageLength` parsed varint size (up to 2^62-1) was directly used for a `make([]byte, size)` allocation.
**Learning:** An attacker can easily trigger massive memory allocation, crashing the application.
**Prevention:** Apply a size check constraint (e.g. 50MB limit) immediately prior to allocation in `Decode` methods.

## 2024-06-21 - Fix OOM DoS via unconstrained varint array slice pre-allocation
**Vulnerability:** A memory exhaustion (OOM) vulnerability caused by lack of bounds checking when pre-allocating slices for variable-length arrays based on an untrusted varint count prefix (e.g., `make([]string, 0, count)` or `make([]uint64, count)`). An attacker can specify a huge count for an array with very few bytes, causing the server to allocate massive amounts of memory and crash before parsing the next item.
**Learning:** Even if the overall message size is constrained or the buffer is small, `make` pre-allocations using unvalidated array lengths can cause OOM DoS.
**Prevention:** Compute a clamped capacity based on `len(b)` and the maximum possible count before allocating, and allocate `make([]T, 0, allocCap)`. Let the subsequent decode loop naturally fail with an EOF when the buffer is exhausted.
## 2024-05-15 - Fix DoS via panic on oversized message inputs
**Vulnerability:** The application used `panic()` when parsing untrustedQUIC messages with excessively large length varints (exceeding math.MaxInt), which allows remote unauthenticated attackers to cause a Denial of Service (DoS) by crashing the server.
**Learning:** Never use `panic()` for control flow or error handling of malformed input from network streams, as this violates secure failover principles. Always return a proper error (e.g., `ErrMessageTooLarge`) and gracefully disconnect the faulty client.
**Prevention:** Use static analysis tools to flag any usage of `panic()` in parsing paths handling untrusted data, and ensure parsing libraries always return errors.
