## 2024-05-23 - [DoS via Untrusted Payload Length Limits]
**Vulnerability:** Found `panic("byte slice too large")` and `panic("string array too large")` being thrown when processing potentially untrusted incoming data.
**Learning:** Returning a `panic` acts as a fast path for Denial of Service if the application doesn't catch it properly at the transport boundary. Also found potential for memory exhaustion attacks where `make([]string, 0, count)` blindly allocated capacity based on parsed user data before checking if `b` has enough elements.
**Prevention:** Handlers for untrusted input should return `error` types rather than panicking. Always validate requested array counts/sizes against buffer sizes (`len(b) < count`) prior to array allocations.
