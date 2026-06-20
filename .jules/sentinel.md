## 2024-06-20 - Unbounded memory allocation in payload Decode

**Vulnerability:** The application was vulnerable to Out-Of-Memory (OOM) Denial of Service (DoS) attacks. Unbounded allocation occurred because varint sizes were read and directly passed to `make([]byte, size)` during payload decoding without checking limits.

**Learning:** When parsing variable length inputs, trusting the encoded length directly can cause memory exhaustion. Testing correctly identifies the maximum parseable integer value in benchmarks, but failed to exercise OOM behavior when the test input omitted the actual bytes.

**Prevention:** Apply a hard upper bound limit before allocating buffers for payloads based on network varints. Limit memory allocation size to prevent DoS.
