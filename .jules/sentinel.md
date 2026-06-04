## 2025-06-04 - [CRITICAL] Prevent panic from out-of-bounds slice allocation
**Vulnerability:** The application was using `panic()` when parsing untrusted input with large lengths, which could crash the program (DoS) if triggered over the network.
**Learning:** `math.MaxInt` is architecture dependent and checks against it may fail or create unreachable code. Parsing untrusted payloads with lengths must validate against the available buffer remaining lengths and return standard errors (e.g. `io.EOF`) instead of panicking.
**Prevention:** Always validate size inputs against available buffer size instead of hardcoded maximums like `math.MaxInt` and return standard errors instead of panicking when parsing untrusted data.
