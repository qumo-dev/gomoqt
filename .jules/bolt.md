## 2025-03-08 - [String byte cast and Heap escapes]
**Learning:** [Using `[]byte(s)` to convert string `s` to a byte slice inside a writer function creates an extra heap allocation. Using stack-allocated buffers in `io.ReadFull` causes escape to heap analysis failures, resulting in extra allocations on reads.]
**Action:** [Use `append(dest, s...)` directly to append a string to a byte slice, saving an allocation. Use type assertion to `io.ByteReader` to read bytes directly without a stack-allocated buffer when reading small variables like varints.]
