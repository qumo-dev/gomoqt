## 2024-05-18 - Prevent OOM DoS in varint payload decoding
**Vulnerability:** The application was vulnerable to Out-Of-Memory (OOM) Denial of Service (DoS) attacks because it directly allocated byte slices `make([]byte, size)` using arbitrary sizes read from varint network lengths without applying any upper limits.
**Learning:** Network length prefixes (varints) cannot be trusted blindly, as attackers can easily supply massive lengths (e.g., 10GB) causing the application to panic and crash immediately.
**Prevention:** Always cap direct allocations derived from network input against a strict application boundary (e.g., `MaxMessageAllocationSize = 50 * 1024 * 1024`) to guarantee memory safety before allocating.
