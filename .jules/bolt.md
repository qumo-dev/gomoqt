## 2024-06-20 - [Fix OOM DoS in frame decode & test overhead]
**Learning:** `BenchmarkFrame_Decode` was failing due to OOM/timeout. This was caused by two issues:
1. `bytes.Repeat(encodedData, b.N+1)` inside the `b.N` loop is extremely bad. `b.N` can grow very large and `Repeat` allocates `O(b.N)` memory causing excessive GC pressure, heap usage and eventually tests timing out / crashing. The fix is to use `reader := bytes.NewReader(encodedData)` and `reader.Reset(encodedData)` inside the loop, eliminating allocations for the repeating reader.
2. The `Frame.decode` logic had a bug. When checking if the payload has enough capacity: `if cap(f.body) < int(num) { f.body = make([]byte, num) } else { f.body = f.body[:num] }`. This fails because `f.body` doesn't retain its original capacity when you call `make([]byte, num)`. The original `f.body` is severed from the underlying `f.buf` array! Subsequent reads use a detached `f.body` which `f.encode` doesn't encode correctly from `f.buf`. The correct fix is to reallocate via `f.init(num)` which properly re-links the body view to the new `f.buf`.

**Action:**
1. Fix test `moqt/frame_benchmark_test.go` by using `reader.Reset` on the single encoded data snippet.
2. Fix `Frame.decode` in `moqt/frame.go` to properly use `f.init(int(num))` instead of `f.body = make([]byte, num)`.
