## 2026-05-22 - [Performance Bottlenecks Identified in GoMOQT]
**Learning:** `context.AfterFunc` memory leaks if the `stop` function isn't properly stored and called when no longer needed. The `moqt` codebase makes heavy use of it for tracking the end of broadcast announcements (`Announcement` instances). In a long running session, an announcement's end-func would otherwise stay attached to the parent Context until the parent's cancellation.
**Action:** When tracking short-lived objects attached to long-lived contexts with `context.AfterFunc`, always maintain the returned `stop` func and call it in the object's cleanup (`end()`) function to prevent memory leaks.

**Learning:** `f.decode()` in `Frame` blindly allocates `f.body = make([]byte, num)` instead of re-slicing the `f.buf` array structure it keeps. This causes extreme allocations but also breaks the `f.encode()` function which assumes `f.buf` has enough capacity (since it calculates `start := 8 - len(header); end := 8 + len(f.body); w.Write(f.buf[start:end])`).
**Action:** Always align slices and backing arrays when implementing decoding to maintain buffer integrity, reduce allocations, and avoid panics.
