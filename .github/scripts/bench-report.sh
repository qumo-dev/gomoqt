#!/usr/bin/env bash
# Build a readable benchmark-comparison PR comment from a benchstat baseline and
# the PR's results. Produces, in order:
#   1. a one-line summary: regression/improvement counts (or "No significant
#      changes");
#   2. a filtered "significant changes" view (only changed benchmarks, with env
#      metadata once);
#   3. the full benchstat table, collapsed, for audit.
#
# Usage: bench-report.sh <baseline.txt> <new.txt> <out.md>
# Requires: benchstat on PATH (go install golang.org/x/perf/cmd/benchstat@latest).
#
# Called by .github/workflows/benchmark-label.yml. Kept as a standalone script
# (rather than inline in the workflow) so it is readable and independently
# testable: `bench-report.sh base.txt new.txt out.md`.
set -euo pipefail

baseline="${1:?usage: bench-report.sh <baseline.txt> <new.txt> <out.md>}"
new="${2:?usage: bench-report.sh <baseline.txt> <new.txt> <out.md>}"
out="${3:?usage: bench-report.sh <baseline.txt> <new.txt> <out.md>}"

work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
full="$work/benchstat-full.txt"; filt="$work/benchstat.txt"

# Full benchstat across all metrics (sec/op, B/op, allocs/op). Default alpha=0.05;
# allocs/op deltas are deterministic -> always significant regardless of -count.
benchstat "$baseline" "$new" > "$full" || true

# Readable subset: keep a metric section only if it has >=1 significant
# per-benchmark change; within kept sections drop the non-significant ("~")
# rows, the geomean row, and benchstat footnotes. Env metadata printed once.
awk '
  function printBlock() {
    if (outCount == 0) printf "%s", envBlock; else print ""
    printf "%s", buf; outCount++
  }
  BEGIN { outCount = 0 }
  /^(goos|goarch|pkg|cpu):/ { envBlock = envBlock $0 "\n"; next }
  /~/ { next }
  /need >= |are equal|must be >0/ { next }
  /^[[:space:]]*$/ { if (emit) printBlock(); buf = ""; emit = 0; next }
  { buf = buf $0 "\n"; if ($0 ~ / [+-][0-9]+\.[0-9]+% / && $0 !~ /^geomean/) emit = 1 }
  END { if (emit) printBlock() }
' "$full" > "$filt"

# Count significant per-benchmark regressions (+) and improvements (-), excluding
# the geomean rows so the summary isn't double-counted.
regress=$(grep -E ' \+[0-9]+\.[0-9]+% ' "$full" | grep -vc '^geomean' || true)
improv=$(grep -E ' -[0-9]+\.[0-9]+% ' "$full" | grep -vc '^geomean' || true)

{
  echo "<!-- bench-report -->"
  echo "## 🏎️ Benchmark comparison (main → PR)"
  echo
  echo "<sub>Microbenchmarks \`-benchtime=50ms -count=10\` (turnaround over precision; significance from -count). \`allocs/op\` and \`B/op\` are deterministic — any delta there is real. \`sec/op\` is a quick signal; re-run locally with \`-benchtime=1s -count=10+\` for significance. In the table, \`+\` = regression, \`-\` = improvement, \`~\` = not significant.</sub>"
  echo
} > "$out"

summary=""
[ "$regress" -gt 0 ] && summary="⚠️ **${regress} regression(s)**"
if [ "$improv" -gt 0 ]; then
  [ -n "$summary" ] && summary="$summary  •  "
  summary="${summary}✅ **${improv} improvement(s)**"
fi
if [ -z "$summary" ]; then
  echo "✅ **No significant changes** — all benchmarks within measurement noise." >> "$out"
else
  echo "$summary" >> "$out"
fi
echo >> "$out"

# Significant changes shown prominently (open); full table collapsed for audit.
if [ "$regress" -gt 0 ] || [ "$improv" -gt 0 ]; then
  {
    echo "<details open>"
    echo "<summary>📊 Significant changes (main → PR, filtered)</summary>"
    echo
    echo '```text'
    cat "$filt"
    echo '```'
    echo "</details>"
  } >> "$out"
fi
{
  echo "<details>"
  echo "<summary>🔎 Full benchstat output (all benchmarks, all metrics)</summary>"
  echo
  echo '```text'
  cat "$full"
  echo '```'
  echo "</details>"
} >> "$out"
