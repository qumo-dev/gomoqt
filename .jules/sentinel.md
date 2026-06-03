## 2025-05-24 - DoS Vulnerability via panic on Untrusted Sizes

**Vulnerability:** A Denial of Service (DoS) vulnerability was present in the network protocol reader `moqt/internal/message/message_reader.go`. It used `panic` to reject payloads or array counts encoded with varints that exceed `math.MaxInt`.
**Learning:** `panic` is fundamentally unsafe for validation of externally untrusted network inputs in Go, especially when the input dictates size allocation bounds. A malicious actor could construct a packet with arbitrary 8-byte varints to force a hard crash. Even if bounded by `math.MaxInt`, memory limits should be enforced relative to available bytes.
**Prevention:** Never use `panic` to enforce length constraints on untrusted network packets. Always compare the requested lengths against the `len()` of remaining buffered bytes. If the request size exceeds available bytes, securely return a standard Go error like `io.EOF` so the session terminates or re-buffers appropriately.
