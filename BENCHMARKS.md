# Benchmarks & performance regression

gomoqt ships benchmarks under `moqt/*_benchmark_test.go`. This document explains
how to run them locally, how CI captures baselines, and how PRs are compared
against those baselines with [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat).

## Quick start (local)

```bash
# 1. Quick sanity check — runs each benchmark once with allocations reporting.
mage bench:short

# 2. Capture a baseline (10 samples, the same shape CI uses).
mage bench:full > bench-baseline.txt

# 3. Make your change, then compare.
mage bench:compare                  # compares against ./bench-baseline.txt
# or point at a specific baseline:
mage bench:compare path/to/baseline.txt
```

You can also run the raw commands directly:

```bash
# Capture a baseline.
go test -run='^$' -bench=. -benchmem -benchtime=1x -count=10 -timeout=20m ./moqt/... \
  | tee bench-baseline.txt

# After your change, capture again...
go test -run='^$' -bench=. -benchmem -benchtime=1x -count=10 -timeout=20m ./moqt/... \
  | tee bench-new.txt

# ...and compare.
go install golang.org/x/perf/cmd/benchstat@latest
benchstat bench-baseline.txt bench-new.txt
```

### Why `-count=10`?

benchstat needs multiple samples per benchmark to distinguish a real change from
noise. With `-count=1` there is no variance estimate and benchstat reports every
delta as significant. `-count=10` is the smallest count that gives a usable
variance estimate while keeping the run affordable.

### Why `-benchtime=1x`?

`1x` runs each benchmark function body exactly once per iteration. The hot loops
are already inside `b.N`, so `1x` exercises the same code paths as the default
`1s` budget at a fraction of the wall-clock cost. For higher-fidelity numbers on
a fast machine, bump to `-benchtime=100ms` or `-benchtime=1s`.

## CI behavior

The [`Go` workflow](.github/workflows/go.yml) has three jobs relevant to
performance:

| Job        | Trigger                                  | Purpose                                                            |
|------------|------------------------------------------|--------------------------------------------------------------------|
| `race`     | push to main, PRs touching Go            | `go test -race ./moqt/...` (needs CGO + gcc; ubuntu runners have it) |
| `benchmark`| push to main, tags `v*`, manual dispatch | Captures `bench-new.txt` and uploads it as the `bench-baseline` artifact (90-day retention) |
| `benchmark`| PRs touching Go                          | Runs benchmarks, downloads the latest main baseline, runs `benchstat`, uploads a `bench-pr-<n>` artifact (14-day retention) |

### How the benchstat comparison works

On a PR, the `benchmark` job:

1. Resolves the most recent successful `push` run of `go.yml` on `main` using
   `gh run list`, then downloads its `bench-baseline` artifact. (The artifact is
   named `bench-new.txt` at upload time; it's renamed to `bench-baseline.txt`
   locally.)
2. Runs `go test -run='^$' -bench=. -benchmem -benchtime=1x -count=10 ./moqt/...`
   to produce `bench-new.txt`.
3. Runs `benchstat baseline/bench-baseline.txt bench-new.txt`. The output table
   has columns for `ns/op`, `B/op`, `allocs/op`, plus variance. `~` means no
   significant change; `+x%` / `-x%` is a drift beyond noise (p < 0.05).

The comparison step is informational — it `continue-on-error`s because benchstat
treats improvements as a non-zero exit too. Reviewers should look at the printed
table and the uploaded `bench-pr-<n>` artifact. If the first PR after a main
merge has no baseline to compare against (no successful main run yet), the step
is skipped gracefully.

### Reading the variance

benchstat's variance columns matter as much as the mean. A benchmark whose
variance jumped several-fold between baseline and PR is unreliable — the "mean
drift" it reports is probably noise. When evaluating an optimization PR:

- Look for **mean drift** (`ns/op`, `B/op`, `allocs/op`) — the actual signal.
- Look for **variance inflation** — if variance roughly doubled, re-run with a
  larger `-benchtime` or higher `-count` before trusting the delta.

## Race detection

`go test -race` requires CGO and a C compiler. The CI `race` job sets
`CGO_ENABLED=1` and relies on `gcc` shipped on `ubuntu-latest`. Local machines
without a C compiler cannot run `-race`; that is expected. Run it via CI or on a
machine that has gcc/clang installed.
