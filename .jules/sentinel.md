## 2025-03-08 - Fix DoS vulnerability via panic in message parsing
**Vulnerability:** The application was using panic() when handling maliciously crafted byte arrays that declared excessively large slice sizes in `ReadBytes` and `ReadStringArray` methods.
**Learning:** Parsing untrusted payload inputs with `panic()` creates a Denial of Service (DoS) vulnerability. When decoding payload sizes, sizes must be validated against the remaining buffer length instead of blindly checking against maximum integers or panicking.
**Prevention:** Never use panic() to handle invalid network input. Always validate requested lengths against available buffer size and return standard errors like `io.EOF` if the requested size exceeds the available buffer.
