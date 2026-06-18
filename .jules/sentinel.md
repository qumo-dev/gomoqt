## 2024-06-18 - Prevent Out-of-Memory (OOM) via Malicious Message Length Varints
**Vulnerability:** A malicious client could send an enormous value inside a QUIC varint (up to 1<<62 - 1) for the MOQT message length prefix. The previous code directly used this unvalidated length value to allocate a slice using `make([]byte, size)`, leading to an immediate Out-Of-Memory (OOM) crash (Denial of Service).
**Learning:** In Go, dynamically sizing slice allocations (`make([]byte, size)`) using unsanitized input originating from the network is a severe DoS vector.
**Prevention:** Always define and validate against a sensible `MaxMessageSize` constant before using network-provided lengths to allocate memory.
