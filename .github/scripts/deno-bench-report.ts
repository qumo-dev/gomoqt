/**
 * Build a readable benchmark-comparison PR comment from two `deno bench --json`
 * runs (base and PR head), captured back-to-back in the same job so the diff
 * reflects code, not runner drift. Mirrors the Go `bench-report.sh` flow for the
 * Deno (`moq-web`) benchmark suite.
 *
 * Usage:
 *   deno run --allow-read --allow-write \
 *     .github/scripts/deno-bench-report.ts <base.json> <pr.json> <out.md>
 *
 * `deno bench --json` has no benchstat equivalent and its `avg` is wall-time on a
 * shared runner, so a single sample is noisy. We therefore treat sec/op as a
 * directional signal only: a change is flagged (⚠️ regression / ✅ improvement)
 * just past a ±10% band; anything inside the band is reported as `~` (noise).
 */

interface BenchResult {
  ok?: { avg: number; min: number; p75: number };
}

interface Bench {
  group?: string;
  name: string;
  results: BenchResult[];
}

interface BenchFile {
  cpu?: string;
  runtime?: string;
  benches: Bench[];
}

// A change must exceed this band (fraction) to be called a regression/improvement.
const THRESHOLD = 0.10;
const MARKER = "<!-- ts-bench-report -->";

function avgByName(file: BenchFile): Map<string, number> {
  const m = new Map<string, number>();
  for (const b of file.benches) {
    const avg = b.results[0]?.ok?.avg;
    if (typeof avg === "number") m.set(b.name, avg);
  }
  return m;
}

function fmtNs(ns: number): string {
  if (ns < 1_000) return `${ns.toFixed(1)} ns`;
  if (ns < 1_000_000) return `${(ns / 1_000).toFixed(2)} µs`;
  return `${(ns / 1_000_000).toFixed(2)} ms`;
}

function read(path: string): BenchFile {
  return JSON.parse(Deno.readTextFileSync(path)) as BenchFile;
}

function main(): void {
  const [basePath, prPath, outPath] = Deno.args;
  if (!basePath || !prPath || !outPath) {
    console.error(
      "usage: deno-bench-report.ts <base.json> <pr.json> <out.md>",
    );
    Deno.exit(2);
  }

  const base = read(basePath);
  const pr = read(prPath);
  const baseAvg = avgByName(base);

  let regress = 0;
  let improv = 0;
  const rows: string[] = [];

  // PR ordering is the source of truth; benches missing from base are "new".
  for (const b of pr.benches) {
    const prVal = b.results[0]?.ok?.avg;
    if (typeof prVal !== "number") continue;
    const baseVal = baseAvg.get(b.name);

    let deltaCell: string;
    let mark = "~";
    if (baseVal === undefined) {
      deltaCell = "_new_";
    } else {
      const frac = (prVal - baseVal) / baseVal;
      const pct = (frac * 100).toFixed(1);
      if (frac > THRESHOLD) {
        mark = "⚠️";
        regress++;
      } else if (frac < -THRESHOLD) {
        mark = "✅";
        improv++;
      }
      deltaCell = `${frac >= 0 ? "+" : ""}${pct}% ${mark}`;
    }

    const group = b.group ?? "";
    rows.push(
      `| ${group} | ${b.name} | ${
        baseVal === undefined ? "—" : fmtNs(baseVal)
      } | ${fmtNs(prVal)} | ${deltaCell} |`,
    );
  }

  let summary: string;
  if (regress === 0 && improv === 0) {
    summary =
      "✅ **No significant changes** — all benchmarks within the ±10% noise band.";
  } else {
    const parts: string[] = [];
    if (regress > 0) parts.push(`⚠️ **${regress} possible regression(s)**`);
    if (improv > 0) parts.push(`✅ **${improv} possible improvement(s)**`);
    summary = parts.join("  •  ");
  }

  const lines: string[] = [
    MARKER,
    "## 🏎️ Deno benchmark comparison (main → PR)",
    "",
    `<sub>\`deno bench\` wall-time on a shared runner — sec/op is a directional signal only (single sample, ±10% band; re-run locally to confirm). CPU: ${
      pr.cpu ?? "?"
    } · ${pr.runtime ?? "?"}</sub>`,
    "",
    summary,
    "",
    "| group | benchmark | base | PR | Δ sec/op |",
    "| --- | --- | --- | --- | --- |",
    ...rows,
  ];

  Deno.writeTextFileSync(outPath, lines.join("\n") + "\n");
  console.log(lines.join("\n"));
}

main();
