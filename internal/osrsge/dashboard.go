package osrsge

import ()

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>OSRS GE Strategy Desk</title>
<style>
:root {
  color-scheme: light;
  --ink: #18202a;
  --muted: #667085;
  --line: #d7dde6;
  --panel: #f7f9fc;
  --accent: #176b87;
  --accent-2: #7a4b12;
  --good: #176c47;
  --warn: #9a5b00;
  --bad: #a83c32;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  color: var(--ink);
  background: #eef2f6;
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 24px;
  background: #ffffff;
  border-bottom: 1px solid var(--line);
}
h1 {
  margin: 0;
  font-size: 20px;
  font-weight: 720;
}
.sub {
  color: var(--muted);
  font-size: 13px;
}
main {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 380px;
  gap: 16px;
  padding: 16px;
}
.panel, .strategy-panel {
  background: #ffffff;
  border: 1px solid var(--line);
  border-radius: 8px;
}
.strategy-panel {
  padding: 12px;
  margin-bottom: 12px;
}
.strategy-panel h2 {
  margin: 0 0 8px;
  font-size: 16px;
}
.strategy-text {
  min-height: 128px;
}
.action-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}
.ghost {
  color: var(--accent);
  background: #e9f4f8;
}
label {
  display: grid;
  gap: 5px;
  color: var(--muted);
  font-size: 12px;
}
input, textarea, select {
  width: 100%;
  border: 1px solid #c9d2df;
  border-radius: 6px;
  padding: 8px 9px;
  color: var(--ink);
  background: #ffffff;
  font: inherit;
}
button {
  border: 0;
  border-radius: 6px;
  padding: 9px 12px;
  color: #ffffff;
  background: var(--accent);
  font-weight: 680;
  cursor: pointer;
}
button.secondary {
  background: var(--accent-2);
}
.tablewrap {
  overflow: auto;
  background: #ffffff;
  border: 1px solid var(--line);
  border-radius: 8px;
}
table {
  width: 100%;
  border-collapse: collapse;
  min-width: 980px;
  font-size: 13px;
}
th, td {
  text-align: left;
  padding: 9px 10px;
  border-bottom: 1px solid #e6eaf0;
  white-space: nowrap;
}
th {
  position: sticky;
  top: 0;
  background: #f3f6fa;
  color: #475467;
  font-size: 12px;
}
.score {
  font-variant-numeric: tabular-nums;
  font-weight: 700;
}
.setup {
  display: inline-block;
  border-radius: 999px;
  padding: 3px 8px;
  background: #e9f4f8;
  color: #135a72;
}
.setup.fresh { background: #e9f7ef; color: var(--good); }
.setup.rebound { background: #fff3df; color: var(--warn); }
.side {
  display: grid;
  gap: 12px;
  align-content: start;
}
.panel {
  padding: 12px;
}
.panel h2 {
  margin: 0 0 10px;
  font-size: 15px;
}
.metric-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.metric {
  background: var(--panel);
  border: 1px solid #e2e7ef;
  border-radius: 6px;
  padding: 8px;
}
.metric b {
  display: block;
  font-size: 18px;
}
textarea {
  min-height: 178px;
  resize: vertical;
  line-height: 1.4;
}
.generated {
  min-height: 240px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
.status {
  min-height: 20px;
  color: var(--muted);
  font-size: 13px;
}
@media (max-width: 980px) {
  main { grid-template-columns: 1fr; }
}
</style>
</head>
<body>
<header>
  <div>
    <h1>OSRS GE Strategy Desk</h1>
    <div class="sub">Natural-language market research for manual Grand Exchange traders</div>
  </div>
  <button id="scan">Run Scan</button>
</header>
<main>
  <section>
    <div class="strategy-panel">
      <h2>Describe The Edge</h2>
      <textarea class="strategy-text" id="strategy_text" spellcheck="false">Looking back at historical values, which items are at the bottom of their VWAP or normal trading range, still have consistent volume, and have a history of rebounding? I have 40m and want to filter out one-off low-volume spikes.</textarea>
      <div class="action-row">
        <button id="compile">Build LLM Search Plan</button>
        <button class="ghost" id="scanFromText">Run Agent Workbench</button>
      </div>
    </div>
    <div class="status" id="status"></div>
    <div class="tablewrap">
      <table>
        <thead>
          <tr id="headrow">
            <th>#</th><th>Item</th><th>Dip</th><th>Rebound</th><th>Net</th><th>ROI</th>
            <th>Dip Vol</th><th>Rebound Vol</th><th>Limit</th><th>40M Edge</th><th>Setup</th><th>Score</th>
          </tr>
        </thead>
        <tbody id="rows"></tbody>
      </table>
    </div>
  </section>
  <aside class="side">
    <div class="panel">
      <h2>Run</h2>
      <div class="metric-grid">
        <div class="metric"><span>Scanned</span><b id="scanned">-</b></div>
        <div class="metric"><span>Hits</span><b id="hits">-</b></div>
        <div class="metric"><span>Errors</span><b id="errors">-</b></div>
        <div class="metric"><span>Cash</span><b id="cashmetric">-</b></div>
      </div>
    </div>
    <div class="panel">
      <h2>LLM Search Plan</h2>
      <textarea id="brief" spellcheck="false"></textarea>
    </div>
    <div class="panel">
      <h2>Generated Strategy Fields</h2>
      <textarea class="generated" id="generated_fields" spellcheck="false" readonly></textarea>
    </div>
  </aside>
</main>
<script>
const $ = (id) => document.getElementById(id);
const fmt = (n) => n == null ? "-" : Number(n).toLocaleString();
const pct = (n) => n == null ? "-" : (Number(n) * 100).toFixed(2) + "%";
const shortTime = (iso) => iso ? new Date(iso).toLocaleString([], {month:"2-digit", day:"2-digit", hour:"2-digit", minute:"2-digit"}) : "-";
let lastReport = null;

function deriveStrategySpec() {
  const text = $("strategy_text").value.toLowerCase();
  const spec = {
    intent: "shock_reversion",
    objective: "find cheap, high-limit items with dump/rebound behavior",
    cash: "40m",
    candidate_limit: "250",
    limit: "20",
    days: "14",
    step: "5m",
    max_low: "80",
    min_high: "60",
    requested_tools: ["scan_patterns"],
    evidence_tests: [
      "tax-adjusted spread",
      "buy-limit adjusted portfolio fit",
      "dip and rebound volume",
      "one-off spike rejection"
    ],
    llm_generated: true
  };
  if (text.includes("vwap") || text.includes("trading range") || text.includes("bottom") || text.includes("historical values")) {
    spec.intent = "range_bottom";
    spec.objective = "find items trading near the bottom of their historical VWAP or normal range";
    spec.days = "90";
    spec.step = "6h";
    spec.candidate_limit = "500";
    spec.limit = "25";
    spec.requested_tools = ["scan_range_bottom", "scan_recurring", "scan_patterns"];
    spec.evidence_tests = [
      "current price percentile vs 30d and 90d range",
      "distance from VWAP or volume-weighted normal band",
      "active trading days and median volume",
      "rebound history after bottom-range visits",
      "single-event concentration rejection",
      "tax-adjusted spread and GE-limit portfolio fit"
    ];
  }
  if (text.includes("quarter") || text.includes("90") || text.includes("recurring") || text.includes("consistent")) {
    spec.days = "90";
    spec.candidate_limit = "500";
    spec.limit = "25";
    if (!spec.requested_tools.includes("scan_recurring")) {
      spec.requested_tools.unshift("scan_recurring");
    }
  }
  if (text.includes("monthly") || text.includes("30d") || text.includes("30 day")) {
    spec.days = "30";
  }
  if (text.includes("year") || text.includes("365")) {
    spec.days = "365";
  }
  const cashMatch = text.match(/(\d+(?:\.\d+)?)\s*m\b/);
  if (cashMatch) {
    spec.cash = cashMatch[1] + "m";
  }
  const lowBand = text.match(/(\d+)\s*[-to]+\s*(\d+)\s*gp/);
  if (lowBand) {
    spec.max_low = lowBand[2];
  }
  $("generated_fields").value = JSON.stringify(spec, null, 2);
  return spec;
}

async function scan() {
  const spec = deriveStrategySpec();
  $("status").textContent = "Running agent workbench probes...";
  $("scan").disabled = true;
  const params = new URLSearchParams({
    query: $("strategy_text").value,
    cash: spec.cash,
    candidate_limit: spec.candidate_limit,
    limit: "6"
  });
  try {
    const res = await fetch("/api/agent/run?" + params.toString());
    const text = await res.text();
    if (!res.ok) throw new Error(text);
    lastReport = JSON.parse(text);
    render(lastReport);
    $("status").textContent = "Updated " + new Date(lastReport.generated_at).toLocaleTimeString();
  } catch (err) {
    $("status").textContent = "Scan failed: " + err.message;
  } finally {
    $("scan").disabled = false;
  }
}

function compilePlan() {
  deriveStrategySpec();
  $("brief").value = buildStrategyPlan(lastReport);
}

function render(report) {
  if (report.strategy === "agent-workbench") {
    $("scanned").textContent = fmt(report.probes.length) + " probes";
    $("hits").textContent = fmt(report.summary_rows.length);
    $("errors").textContent = fmt(report.probes.filter((probe) => !probe.ok).length);
    $("cashmetric").textContent = report.spec.cash || "-";
    $("generated_fields").value = JSON.stringify(report.spec, null, 2);
    $("headrow").innerHTML = "<th>#</th><th>Probe</th><th>Item</th><th>Metric</th><th>Evidence</th><th>Setup</th><th>Score</th>";
    $("rows").innerHTML = report.summary_rows.map((row) =>
      "<tr>" +
        "<td>" + row.rank + "</td><td>" + row.probe + "</td><td>" + row.item + "</td>" +
        "<td>" + row.primary_metric + "</td><td>" + row.evidence + "</td>" +
        "<td><span class=\"setup\">" + row.setup + "</span></td><td class=\"score\">" + Number(row.score).toFixed(1) + "</td>" +
      "</tr>"
    ).join("");
    $("brief").value = buildStrategyPlan(report);
    return;
  }
  $("scanned").textContent = fmt(report.scanned) + "/" + fmt(report.candidate_rows);
  $("hits").textContent = fmt(report.hits.length);
  $("errors").textContent = fmt(report.errors);
  $("cashmetric").textContent = fmt(report.cash);
  if (report.strategy === "range-bottom") {
    $("headrow").innerHTML = "<th>#</th><th>Item</th><th>Current</th><th>VWAP</th><th>Percentile</th><th>Discount</th>" +
      "<th>Median Vol</th><th>Active</th><th>Cycles</th><th>40M Edge</th><th>Setup</th><th>Score</th>";
    $("rows").innerHTML = report.hits.map((hit) => {
      const cls = hit.setup.startsWith("Deep") ? "fresh" : hit.setup.startsWith("Recurring") ? "rebound" : "";
      return "<tr>" +
        "<td>" + hit.rank + "</td><td>" + hit.name + "</td><td>" + Number(hit.current_price).toFixed(1) + "</td>" +
        "<td>" + Number(hit.vwap).toFixed(1) + "</td><td>" + pct(hit.percentile) + "</td>" +
        "<td>" + pct(hit.discount_to_vwap) + "</td><td>" + fmt(hit.median_volume) + "</td>" +
        "<td>" + fmt(hit.active_buckets) + "/" + fmt(hit.observed_buckets) + "</td>" +
        "<td>" + fmt(hit.rebound_cycles) + "/" + fmt(hit.bottom_visits) + "</td>" +
        "<td>" + fmt(hit.portfolio_profit_to_vwap) + "</td>" +
        "<td><span class=\"setup " + cls + "\">" + hit.setup + "</span></td><td class=\"score\">" + Number(hit.score).toFixed(1) + "</td>" +
      "</tr>";
    }).join("");
  } else {
    $("headrow").innerHTML = "<th>#</th><th>Item</th><th>Dip</th><th>Rebound</th><th>Net</th><th>ROI</th>" +
      "<th>Dip Vol</th><th>Rebound Vol</th><th>Limit</th><th>40M Edge</th><th>Setup</th><th>Score</th>";
    $("rows").innerHTML = report.hits.map((hit) => {
    const cls = hit.setup.startsWith("Fresh") ? "fresh" : hit.setup.startsWith("Rebound") ? "rebound" : "";
    return "<tr>" +
      "<td>" + hit.rank + "</td><td>" + hit.name + "</td><td>" + fmt(hit.low) + " @ " + shortTime(hit.low_time_iso) + "</td>" +
      "<td>" + fmt(hit.high) + " @ " + shortTime(hit.high_time_iso) + "</td><td>" + fmt(hit.net_margin) + "</td>" +
      "<td>" + pct(hit.roi) + "</td><td>" + fmt(hit.low_volume) + "</td><td>" + fmt(hit.high_volume) + "</td>" +
      "<td>" + fmt(hit.buy_limit) + "</td><td>" + fmt(hit.portfolio_profit) + "</td>" +
      "<td><span class=\"setup " + cls + "\">" + hit.setup + "</span></td><td class=\"score\">" + Number(hit.score).toFixed(1) + "</td>" +
    "</tr>";
  }).join("");
  }
  $("brief").value = buildStrategyPlan(report);
}

function buildStrategyPlan(report) {
  const strategyText = $("strategy_text").value.trim();
  const spec = report && report.strategy === "agent-workbench" ? report.spec : deriveStrategySpec();
  let top = "";
  if (report && report.strategy === "agent-workbench") {
    top = report.summary_rows.slice(0, 12).map((row) =>
      row.rank + ". [" + row.probe + "] " + row.item + ": " + row.primary_metric +
      " | " + row.evidence + " | " + row.setup
    ).join("\n");
  } else if (report && report.hits && report.strategy === "range-bottom") {
    top = report.hits.slice(0, 8).map((hit) =>
      hit.rank + ". " + hit.name + ": current " + Number(hit.current_price).toFixed(1) +
      ", VWAP " + Number(hit.vwap).toFixed(1) + ", pctl " + pct(hit.percentile) +
      ", discount " + pct(hit.discount_to_vwap) + ", cycles " + hit.rebound_cycles + "/" + hit.bottom_visits +
      ", median vol " + fmt(hit.median_volume) + ", setup=" + hit.setup
    ).join("\n");
  } else if (report && report.hits) {
    top = report.hits.slice(0, 8).map((hit) =>
      hit.rank + ". " + hit.name + ": " + fmt(hit.low) + " -> " + fmt(hit.high) +
      ", net " + fmt(hit.net_margin) + " gp/ea, " + pct(hit.roi) + " ROI, " +
      fmt(hit.portfolio_profit) + " gp 40M-limit-cycle edge, setup=" + hit.setup
    ).join("\n");
  }
  const scanLine = report && report.strategy === "agent-workbench" ?
    "Current workbench run: " + report.probes.length + " probes, " + report.summary_rows.length + " summary rows.\n\n" :
    report ?
    "Current scan: " + report.strategy + ", " + report.days + "d lookback, " + report.scanned + "/" + report.candidate_rows + " candidates scanned.\n\n" :
    "Current scan: not run yet. Generate several candidate scans before answering.\n\n";
  return "You are the LLM strategy compiler for an OSRS Grand Exchange research product.\n\n" +
    "User request:\n" + strategyText + "\n\n" +
    "Generated strategy fields:\n" + JSON.stringify(spec, null, 2) + "\n\n" +
    "Interpret the request into backend scans. The text above is the only user-authored query; all advanced fields are LLM-generated and may be revised after evidence comes back.\n\n" +
    "Execution plan:\n" +
    "1. Use the agent manifest and generated tool list to fan out scans over 30d, 90d, and 365d as relevant.\n" +
    "2. For VWAP/range-bottom intent, rank items by current price percentile, distance below normal range, volume consistency, and rebound history.\n" +
    "3. For dump/rebound intent, rank items by repeated cycles, median event volume, tax-adjusted edge, and GE-limit fit.\n" +
    "4. Reject candidates where one spike, thin volume, stale data, or permanent repricing explains the signal.\n\n" +
    scanLine +
    "Visible agent evidence:\n" + (top || "No routed hits available yet.") + "\n\n" +
    "Return: recommended strategy template, exact scanner parameter sweeps to run, top candidates with evidence, rejected traps, and watch rules. " +
    "Keep the product read-only: no account credentials, client automation, or offer placement.";
}

$("scan").addEventListener("click", scan);
$("scanFromText").addEventListener("click", scan);
$("compile").addEventListener("click", compilePlan);
scan();
</script>
</body>
</html>`
