## 2025-06-10 - Slice append optimization

**Learning:** When generating multiple slice appends, especially in a loop or sequentially, Go's dynamic slice capacity growth strategy creates multiple heap allocations. This becomes a bottleneck in hot paths. Furthermore, converting strings to `[]byte` during appending causes another unnecessary allocation. Also, when tasked with optimizing a specific function (like `WriteVarint` here), ensuring we patch that exact target is key for the reviewer (and the logic) in addition to applying general improvements.

**Action:** Always pre-calculate required buffer length and use `slices.Grow(dest, length)` prior to consecutive appends. In string-to-byte slice appends, prefer `append(dest, s...)` instead of `append(dest, []byte(s)...)`.
