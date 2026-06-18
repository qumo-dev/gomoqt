## 2024-06-25 - Avoid String to Byte Slice Casts for Writing Strings
**Learning:** In Go, converting a string to a byte slice using `[]byte(s)` triggers an allocation on the heap because the resulting byte slice is mutable while the string is not.
**Action:** When appending a string to a byte slice, use the built-in behavior of `append` which natively accepts strings for zero-allocation appends: `append(buf, s...)`. This significantly reduces memory allocations and garbage collection overhead in hot paths.

## 2024-06-25 - Dangers of Unsafe String Conversion for Zero-Copy Reads
**Learning:** Returning `unsafe.String(unsafe.SliceData(b), len(b))` from a parsing utility function is extremely dangerous if the input buffer `b` is transient or reused (e.g., in a tight network read loop). Because the returned string shares the underlying array with the buffer, subsequent reads into that buffer will mutate the supposedly immutable string, causing mysterious data corruption.
**Action:** Avoid `unsafe.String` for zero-copy reads unless you have strict, complete control over the input buffer's lifecycle and guarantee it will not be overwritten while the string is in use. In general-purpose parsing utilities, always accept the single allocation of `string(b)` to guarantee memory safety.
