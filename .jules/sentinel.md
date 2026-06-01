## 2025-06-01 - [CRITICAL] Untrusted Input OOM DoS via Slice Pre-allocation
**Vulnerability:** In `moqt/internal/message/message_reader.go`, `ReadStringArray` allocated a string slice using an untrusted input `count` without verifying if enough bytes remained in the buffer (`arr := make([]string, 0, count)`). This allows an attacker to send a malicious `count` causing an Out-Of-Memory (OOM) panic, resulting in a Denial of Service.
**Learning:** Network protocols that decode arrays or strings from untrusted input must not blindly trust the provided length prefixes for memory allocation.
**Prevention:** Always validate size/count prefixes against the remaining buffer length (`uint64(len(b))`) or a strict maximum limit *before* making any allocations.
