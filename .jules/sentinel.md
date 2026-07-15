## 2024-07-15 - Prevent DoS from untrusted large QUIC varints triggering panic

**Vulnerability:** The application was vulnerable to an unrecoverable Denial of Service (DoS) attack when parsing oversized byte slices and string array counts in `moqt/internal/message/message_reader.go`. A maliciously crafted QUIC varint length prefix could cause a process crash via a `panic` when it exceeded integer bounds.

**Learning:** When reading bounds from untrusted networks in Go, explicitly returning errors (e.g., `ErrMessageTooLarge`) ensures the application degrades gracefully. Relying on `panic()` for control flow or boundary enforcement introduces a trivial remote DoS vector. Furthermore, hardcoded thresholds like `math.MaxInt` are often incorrect or too large; using a domain-specific bound like `MaxMessageSize` is required to prevent upstream allocations that could lead to OOM.

**Prevention:** Ensure that network parsing routines never use `panic()` to handle malformed input or sizes. Always use bounds checks against a safe threshold (like `MaxMessageSize`) and return robust error types to cleanly terminate the malicious stream/connection.
