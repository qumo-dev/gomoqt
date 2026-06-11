## 2024-05-18 - Fix DoS Panic in Message Reader
**Vulnerability:** The message reader used `panic()` when parsing network data lengths (from varints) that exceeded `math.MaxInt`. This creates a Denial of Service (DoS) vulnerability via Out-Of-Memory (OOM) crashes or process termination from crafted payloads.
**Learning:** Using `panic()` for bounds checking or invalid lengths in untrusted payload inputs is dangerous as it crashes the entire process.
**Prevention:** Always return standard errors (e.g., `errors.New()`, `io.EOF`) to safely handle failure states when decoding payload sizes. Additionally, enforce sensible, absolute upper boundaries and validate requested sizes against the remaining buffer length.
