
## 2024-05-31 - [High] Prevent DoS from panics on excessive slice allocation sizes
**Vulnerability:** Parsing functions `ReadBytes` and `ReadStringArray` in `moqt/internal/message/message_reader.go` contained `panic()` calls for unconstrained input sizes, and lacked adequate bounds checking before slice allocations (`make([]string, 0, count)`), exposing the application to DoS attacks via CPU/memory exhaustion or crashes.
**Learning:** Functions dealing with untrusted payload sizes must validate the size against the actual remaining buffer length *before* making memory allocations, and never use panics for malformed data.
**Prevention:** Propagate input validation errors (e.g. `io.EOF`) and avoid excessive allocations using buffer-length bounds checking.
