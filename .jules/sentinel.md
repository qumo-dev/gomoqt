## 2024-05-18 - Replacing panics with errors on malicious size limits
**Vulnerability:** Use of `panic` to handle unexpectedly large length fields (such as varints exceeding max integer values or string bounds checks) when parsing user input.
**Learning:** `panic` crashes the program entirely. An attacker who sends a deliberately malformed payload with an enormous length prefix can easily trigger this panic and crash the service, leading to a Denial of Service (DoS).
**Prevention:** Never use `panic` on bounds/size checks when parsing unvalidated input. Always return standard `errors.New(...)` so the error propagates and the malicious connection/stream can be closed gracefully without affecting the whole server.
