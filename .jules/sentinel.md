## 2025-02-12 - Critical DoS in untrusted payload parsing
**Vulnerability:** Parsing untrusted payload sizes and strings threw a `panic()` when `num > math.MaxInt` rather than gracefully returning an error.
**Learning:** Using `panic()` in parsing untrusted binary formats opens the application to Denial of Service (DoS) attacks, because an attacker can crash the entire application just by sending a payload with an maliciously large size.
**Prevention:** Never use `panic()` for parsing input data, and always validate bounds gracefully returning standard errors.
