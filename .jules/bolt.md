## 2024-05-24 - Path Splitting Allocation Optimization
**Learning:** For `BroadcastPath` splitting (which separates the prefix segments from the final track name), the previous implementation unconditionally split the entire path and re-sliced to drop the last element (`segments[:len(segments)-1]`). Finding the last slash using `strings.LastIndexByte` and counting the remaining slashes to avoid `strings.Split` when there are 0 or 1 slashes reduces string allocations and improves path splitting time significantly, especially since the typical `BroadcastPath` in `TrackMux` has 1 to 3 segments.
**Action:** Always pre-calculate where to split strings using `strings.LastIndexByte` or `strings.IndexByte` for simple, predictable segment extractions to avoid creating intermediate slices and then throwing away the last element, especially in hot paths like `TrackMux` routing.
## 2024-06-25 - Exact pre-allocation over fixed bounds
**Learning:** For variable depth path splitting (e.g. prefix arrays), pre-calculating the exact number of segments via `strings.Count(str, "/")` and allocating the slice exactly (`make([]T, 0, n)`) is measurably faster than fixed pre-allocation (e.g., `make([]T, 0, 8)`). Removing the final `strings.Split` allocation in the `pathSegments` function further reduces GC pressure.
**Action:** Always count known delimiters in small string parsing rather than falling back to `strings.Split` or using arbitrary fixed pre-allocations when generating slices in hot paths.
## 2026-07-01 - Defer String Formatting in Validation Loops

**What:** Deferred string formatting (`fmt.Sprintf` and concatenation) in MSF catalog validation loops, moving the per-entry prefix generation onto the error path only.
**Why:** `fmt.Sprintf("tracks[%d]", i)` ran on every iteration regardless of whether problems existed, allocating on the happy path.
**Impact (measured, `-benchtime=1s -count=10`, benchstat vs main):** `Catalog_Validate` -57% (430->184 ns/op), `CatalogDelta_Validate` -88% (265->32 ns/op); both 3->0 allocs/op, 48->0 B/op (p=0.000).
**Measurement:** Go benchmarks (`msf/catalog_benchmark_test.go`).
## 2024-05-19 - Safe Varint Decoding Optimization
**Learning:** In Go, fallback paths (like non-`io.ByteReader` readers) in hot decoding loops shouldn't use dynamic slice allocations (e.g., `make([]byte, size)`) if the maximum size is small and fixed (like an 8-byte varint). Replacing `make()` with a local fixed-size array (e.g., `var buf [8]byte`) and slicing it `buf[:size]` entirely eliminates heap allocations.
**Action:** When parsing small, bounded objects like varints from an `io.Reader`, use stack-allocated arrays and take their slices (`buf[:length]`) instead of dynamically allocating slices with `make()`.

## YYYY-MM-DD - [Zero-allocation String Write]
**Learning:** In Go, replacing `WriteBytes(dest, []byte(s))` with `append(dest, s...)` avoids string-to-bytes conversion overhead and reduces benchmark time.
**Action:** Use `append` for string writing optimization.

## 2026-07-19 - Context Synchronization in Shutdown Polling
**Learning:** Replacing a `time.Sleep` loop in a goroutine with an event-driven `select` statement blocking on `context.Done()` drastically improves execution latency and eliminates unnecessary CPU busy-waiting, reducing simulated server shutdown benchmarks from ~100.5ms/op to ~5.4ms/op.
**Action:** When creating asynchronous shutdown watchers or lifecycle loops, avoid `time.Sleep` polling in favor of `context.WithCancel` and `select` mechanisms for precise, immediate orchestration.
## YYYY-MM-DD - Avoid Map Initialization Overhead for Small Lists
**Learning:** For finding duplicates or validating uniqueness in very small slices (e.g., N <= 16), an O(N^2) nested loop comparison against a stack-allocated array is measurably faster than using a `map[T]struct{}`. Even if escape analysis optimizes the map to the stack, the nested loop avoids map initialization and element hashing overhead on the happy path.
**Action:** Provide a fast-path fallback using a small, stack-allocated array and O(N^2) loop for uniqueness detection on small bounded items before falling back to a map.
