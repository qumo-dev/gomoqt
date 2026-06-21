## 2024-06-21 - String encoding allocations
**Learning:** In Go, passing a `string` to a function that accepts `[]byte` by doing `[]byte(s)` forces a heap allocation, even if the bytes are immediately appended. However, using `append([]byte, s...)` is handled natively by the compiler and performs a zero-allocation direct copy from the string to the slice.
**Action:** When appending strings to a byte slice in hot paths like network encoding (`WriteString` or `WriteStringArray`), always use `append(dest, s...)` directly instead of converting to bytes or passing to byte-writing functions.
