import { buildStrategyDrop } from "@/lib/strategies";
import { buildShockReversionRows, fallbackShockReversionRows, shockReversionCommands } from "@/lib/reversion";

export type EvidenceRow = {
  probe: string;
  item: string;
  metric: string;
  evidence: string;
  setup: string;
  score: number;
  followUpCommand?: string;
  watchRuleCommand?: string;
};

export type ThesisCandidate = {
  item: string;
  setup: string;
  evidence: string;
  risk: string;
  followUpCommand: string;
  watchRuleCommand?: string;
  confidence: "low" | "medium" | "high";
  sourceProbe: string;
};

export type ThesisBrief = {
  mode: "deterministic-preview" | "deepseek";
  model?: string;
  query: string;
  generatedAt: string;
  queryIntent: string;
  thesis: string;
  candidates: ThesisCandidate[];
  rejectedTraps: string[];
  nextCliCommands: string[];
  caveats: string[];
  usage?: {
    prompt_tokens?: number;
    completion_tokens?: number;
    total_tokens?: number;
  };
};

type DeepSeekChoice = {
  message?: {
    content?: string;
  };
};

type DeepSeekResponse = {
  choices?: DeepSeekChoice[];
  usage?: ThesisBrief["usage"];
};

const allowedModels = new Set(["deepseek-v4-flash", "deepseek-v4-pro"]);

const fallbackEvidenceRows: EvidenceRow[] = [
  {
    probe: "range-bottom-90d",
    item: "Blood rune",
    metric: "286 gp vs 311 gp VWAP",
    evidence: "18th percentile, 8.0% below VWAP, median volume 1.9m, rebound cycles 4/7",
    setup: "Recurring range-bottom",
    score: 82.4,
  },
  {
    probe: "movers-1h",
    item: "Prayer potion(4)",
    metric: "+3.1% price move",
    evidence: "volume ratio 2.8x with current net margin 94 gp",
    setup: "Short-term volume expansion",
    score: 76.1,
  },
  {
    probe: "opportunities-now",
    item: "Rune bar",
    metric: "118 gp net / 2.9% ROI",
    evidence: "fresh low/high, volume 352k, buy limit 10k",
    setup: "Current margin candidate",
    score: 71.8,
  },
  {
    probe: "range-bottom-90d",
    item: "Dragon bones",
    metric: "2,196 gp vs 2,307 gp VWAP",
    evidence: "23rd percentile, 4.8% below VWAP, median volume 811k, rebound cycles 3/5",
    setup: "Liquid bottom watch",
    score: 69.9,
  },
  {
    probe: "patterns-recent",
    item: "Oak plank",
    metric: "410 -> 456 gp",
    evidence: "net 37 gp, low volume 174k, high volume 228k, event repeated twice",
    setup: "Dump/rebound candidate",
    score: 63.2,
  },
];

export function validateScopedQuery(query: string): string | null {
  const normalized = query.trim().toLowerCase();
  if (normalized.length < 12) {
    return "Query is too short for a GE thesis.";
  }
  const blocked = [
    "ignore previous",
    "system prompt",
    "jailbreak",
    "developer message",
    "write code",
    "shell script",
    "email",
    "resume",
    "homework",
    "weather",
    "stock",
    "crypto",
    "bitcoin",
    "password",
    "credential",
    "bot client",
    "automate client",
    "rwt",
  ];
  for (const term of blocked) {
    if (normalized.includes(term)) {
      return `GE Thesis is scoped to OSRS market research. Blocked term: ${term}`;
    }
  }
  const allowed = [
    "osrs",
    "grand exchange",
    " ge ",
    "item",
    "items",
    "flip",
    "flipping",
    "margin",
    "spread",
    "volume",
    "turnover",
    "liquidity",
    "vwap",
    "trend",
    "range",
    "breakout",
    "bullish",
    "rebound",
    "dump",
    "mover",
    "spike",
    "buy",
    "sell",
    "buy limit",
    "limit buy",
    "limit sell",
    "tax",
    "gp",
    "bankroll",
    "rune",
    "potion",
    "bones",
    "lava",
    "dragon",
    "bond",
    "f2p",
    "p2p",
  ];
  const padded = ` ${normalized} `;
  if (!allowed.some((term) => padded.includes(term))) {
    return "Include an OSRS GE market, item, margin, volume, VWAP, bankroll, or flip intent.";
  }
  return null;
}

export async function buildThesisBrief(query: string): Promise<ThesisBrief> {
  const scopeError = validateScopedQuery(query);
  if (scopeError) {
    throw new Error(scopeError);
  }
  const rows = await evidenceRowsForQuery(query);

  const apiKey = process.env.OSRS_GE_DEEPSEEK_API_KEY || process.env.DEEPSEEK_API_KEY;
  if (!apiKey) {
    return deterministicBrief(query, undefined, rows);
  }

  const model = process.env.OSRS_GE_DEEPSEEK_MODEL || "deepseek-v4-flash";
  if (!allowedModels.has(model)) {
    throw new Error("DeepSeek model is not allowlisted for GE Thesis.");
  }

  const response = await fetch("https://api.deepseek.com/chat/completions", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${apiKey}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      model,
      messages: [
        { role: "system", content: systemPrompt() },
        { role: "user", content: userPrompt(query, rows) },
      ],
      response_format: { type: "json_object" },
      max_tokens: 1100,
      temperature: 0,
      stream: false,
      thinking: model === "deepseek-v4-flash" ? { type: "disabled" } : undefined,
    }),
  });

  if (!response.ok) {
    const text = await response.text();
    return deterministicBrief(query, `DeepSeek API ${response.status}: ${summarizeDeepSeekError(text)}`, rows);
  }

  const payload = (await response.json()) as DeepSeekResponse;
  const content = payload.choices?.[0]?.message?.content;
  if (!content) {
    throw new Error("DeepSeek returned an empty response.");
  }

  const firstPass = normalizeDeepSeekBrief(content, { model, query, usage: payload.usage });
  if (firstPass.ok) return alignBriefToIntent(firstPass.brief, query, rows);

  const repair = await repairDeepSeekContent(apiKey, model, query, content);
  if (repair.ok) {
    const repaired = normalizeDeepSeekBrief(repair.content, {
      model,
      query,
      usage: combineUsage(payload.usage, repair.usage),
    });
    if (repaired.ok) return alignBriefToIntent(repaired.brief, query, rows);
    return deterministicBrief(query, `${firstPass.error}; repair failed: ${repaired.error}`, rows);
  }
  return deterministicBrief(query, `${firstPass.error}; repair failed: ${repair.error}`, rows);
}

function deterministicBrief(query: string, modelFallback?: string, rows = fallbackEvidenceRows): ThesisBrief {
  const intent = deriveIntent(query);
  const pickedRows = pickRows(query, rows);
  const shockReversion = intent === "shock_reversion";
  const caveats = [
    "Preview mode uses deterministic OSRS Wiki evidence while DeepSeek synthesis is unavailable or recovering.",
    "Public aggregate data cannot prove personal fills.",
  ];
  if (modelFallback) {
    caveats.unshift(`DeepSeek fallback: ${modelFallback}`);
  }
  return {
    mode: "deterministic-preview",
    query,
    generatedAt: new Date().toISOString(),
    queryIntent: intent,
    thesis:
      shockReversion
        ? shockReversionThesis()
        : "The current preview favors liquid evidence rows with repeatable volume and explicit invalidation. Treat the output as a monitored watchlist, not an execution signal.",
    candidates: pickedRows.slice(0, 3).map(candidateFromRow),
    rejectedTraps: [
      "Thin-volume rows with stale high or low prices",
      "One-off dump rows without repeated rebound cycles",
      "Large spreads that disappear after GE tax",
    ],
    nextCliCommands: nextCommandsForIntent(query, intent),
    caveats,
  };
}

function alignBriefToIntent(brief: ThesisBrief, query: string, rows: EvidenceRow[]): ThesisBrief {
  const intent = deriveIntent(query);
  if (intent !== "shock_reversion") return brief;

  const evidenceCandidates = pickRows(query, rows).slice(0, 3).map(candidateFromRow);
  const generatedByItem = new Map(brief.candidates.map((candidate) => [candidate.item.toLowerCase(), candidate]));
  return {
    ...brief,
    queryIntent: intent,
    thesis: brief.thesis.toLowerCase().includes("bronze knife") ? brief.thesis : shockReversionThesis(),
    candidates: evidenceCandidates.map((candidate) => {
      const generated = generatedByItem.get(candidate.item.toLowerCase());
      return {
        ...candidate,
        risk: generated?.risk || candidate.risk,
        confidence: generated?.confidence || candidate.confidence,
      };
    }),
    nextCliCommands: shockReversionCommands(query),
    rejectedTraps: brief.rejectedTraps.length
      ? brief.rejectedTraps
      : [
          "Thin-volume rows with stale high or low prices",
          "One-off dump rows without repeated rebound cycles",
          "Large spreads that disappear after GE tax",
        ],
    caveats: uniqueStrings([
      ...brief.caveats,
      "Shock-reversion prompts are pinned to Bronze-knife-style CLI pattern evidence before LLM synthesis.",
    ]),
  };
}

function shockReversionThesis() {
  return "This scan should use the Bronze-knife-style shock reversion path: cheap items, high GE buy limits, recent dump-to-rebound behavior, event volume on both legs, tax-adjusted edge, and bankroll fit.";
}

function candidateFromRow(row: EvidenceRow): ThesisCandidate {
  return {
    item: row.item,
    setup: row.setup,
    evidence: row.evidence,
    risk: riskForRow(row),
    followUpCommand: row.followUpCommand || followUpForRow(row),
    watchRuleCommand: row.watchRuleCommand || `osrs-ge watch add "${row.item}" --min-volume 100000`,
    confidence: row.score >= 75 ? "high" : "medium",
    sourceProbe: row.probe,
  };
}

function riskForRow(row: EvidenceRow) {
  if (row.probe.includes("patterns")) {
    return "One-off shock prints can fail if rebound-side volume disappears or the low print cannot fill.";
  }
  if (row.probe === "movers-1h") return "Fresh flow can reverse before the next interval.";
  return "Historical discount can persist without a catalyst.";
}

function followUpForRow(row: EvidenceRow) {
  if (row.probe.includes("breakout")) return "osrs-ge breakouts --limit 8 --min-turnover 40m --json";
  if (row.probe.includes("patterns")) return 'osrs-ge patterns --like "Bronze knife" --cash 40m --days 7 --step 5m --limit 15 --json';
  return `osrs-ge timeseries "${row.item}" --step 1h --limit 168`;
}

function uniqueStrings(values: string[]) {
  return Array.from(new Set(values.filter(Boolean)));
}

function summarizeDeepSeekError(raw: string) {
  try {
    const parsed = JSON.parse(raw) as { error?: { message?: string; code?: string } };
    return parsed.error?.message || parsed.error?.code || "request failed";
  } catch {
    return raw.slice(0, 120);
  }
}

function normalizeDeepSeekBrief(
  content: string,
  meta: Pick<ThesisBrief, "query"> & Pick<Partial<ThesisBrief>, "model" | "usage">,
):
  | { ok: true; brief: ThesisBrief }
  | { ok: false; error: string } {
  try {
    const parsed = parseDeepSeekObject(content) as Record<string, unknown>;
    const normalized = normalizeBrief(parsed, {
      mode: "deepseek",
      model: meta.model,
      query: meta.query,
      usage: meta.usage,
    });
    validateGeneratedCommands(normalized);
    return { ok: true, brief: normalized };
  } catch (error) {
    return {
      ok: false,
      error: error instanceof Error ? error.message : "DeepSeek output could not be validated.",
    };
  }
}

async function repairDeepSeekContent(apiKey: string, model: string, query: string, invalidJson: string) {
  const response = await fetch("https://api.deepseek.com/chat/completions", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${apiKey}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      model,
      messages: [
        {
          role: "system",
          content:
            "Repair malformed JSON for the scoped GE Thesis OSRS Grand Exchange research app. Return strict JSON only. Preserve supplied OSRS GE market content. Do not add non-OSRS content, shell commands, gameplay automation, credentials, scraping, RWT, or gambling. Commands must start with osrs-ge and stay within research CLI commands.",
        },
        { role: "user", content: repairPrompt(query, invalidJson) },
      ],
      response_format: { type: "json_object" },
      max_tokens: 1100,
      temperature: 0,
      stream: false,
      thinking: model === "deepseek-v4-flash" ? { type: "disabled" } : undefined,
    }),
  });

  if (!response.ok) {
    const text = await response.text();
    return { ok: false as const, error: `DeepSeek repair API ${response.status}: ${summarizeDeepSeekError(text)}` };
  }

  const payload = (await response.json()) as DeepSeekResponse;
  const content = payload.choices?.[0]?.message?.content;
  if (!content) {
    return { ok: false as const, error: "DeepSeek repair returned an empty response." };
  }
  return { ok: true as const, content, usage: payload.usage };
}

function repairPrompt(query: string, invalidJson: string) {
  return JSON.stringify({
    query,
    invalidJson: invalidJson.slice(0, 12000),
    requiredShape: {
      queryIntent: "short intent label",
      thesis: "one concise evidence-backed paragraph",
      candidates: [
        {
          item: "item name",
          setup: "setup type",
          evidence: "specific evidence from supplied rows",
          risk: "main invalidation",
          followUpCommand: "osrs-ge timeseries \"item name\" --step 1h --limit 168",
          watchRuleCommand: "osrs-ge watch add \"item name\" --min-volume 100000",
          confidence: "low|medium|high",
          sourceProbe: "probe name",
        },
      ],
      rejectedTraps: ["trap to avoid"],
      nextCliCommands: ["osrs-ge doctor"],
      caveats: ["public aggregate data cannot prove fills"],
    },
  });
}

function combineUsage(first?: ThesisBrief["usage"], second?: ThesisBrief["usage"]) {
  if (!first && !second) return undefined;
  return {
    prompt_tokens: (first?.prompt_tokens || 0) + (second?.prompt_tokens || 0),
    completion_tokens: (first?.completion_tokens || 0) + (second?.completion_tokens || 0),
    total_tokens: (first?.total_tokens || 0) + (second?.total_tokens || 0),
  };
}

async function evidenceRowsForQuery(query: string) {
  const intent = deriveIntent(query);
  if (intent === "shock_reversion") {
    try {
      return await buildShockReversionRows();
    } catch {
      return fallbackShockReversionRows();
    }
  }

  try {
    const drop = await buildStrategyDrop();
    const liveRows = drop.strategies.map((strategy) => ({
      probe: "breakout-1-3m",
      item: strategy.item,
      metric: `${strategy.trend30d} 30d / ${strategy.trend90d} 90d`,
      evidence: [
        strategy.catalyst,
        `${strategy.distanceFromHigh} below the 90-day high.`,
        `Limit buy ${strategy.limitBuy}; sell ${strategy.limitSell}; stretch ${strategy.stretchSell}; invalidation ${strategy.activeInvalidation}.`,
        `Brief P&L ${strategy.briefPnl}, ${strategy.briefROI}, ${strategy.briefLimitProfit}; live spread ${strategy.liveSpread}; break-even ${strategy.breakEvenSell}; tax drag ${strategy.taxDrag}; ${strategy.dataFreshness}.`,
      ].join(" "),
      setup: strategy.setup,
      score: strategy.heat,
    }));
    const normalized = query.toLowerCase();
    return [...liveRows, ...fallbackEvidenceRows].sort((a, b) => {
      const aHit = normalized.includes(a.item.toLowerCase()) ? 1 : 0;
      const bHit = normalized.includes(b.item.toLowerCase()) ? 1 : 0;
      return bHit - aHit || b.score - a.score;
    });
  } catch {
    return fallbackEvidenceRows;
  }
}

function parseDeepSeekObject(content: string) {
  const trimmed = content.trim();
  const unfenced = trimmed
    .replace(/^```(?:json)?\s*/i, "")
    .replace(/\s*```$/i, "")
    .trim();
  const start = unfenced.indexOf("{");
  const end = unfenced.lastIndexOf("}");
  const objectText = start >= 0 && end > start ? unfenced.slice(start, end + 1) : unfenced;
  const attempts = [
    objectText,
    removeTrailingCommas(objectText),
    repairMissingCommas(removeTrailingCommas(objectText)),
  ];
  let lastError: unknown;

  for (const attempt of Array.from(new Set(attempts))) {
    try {
      return JSON.parse(attempt);
    } catch (error) {
      lastError = error;
    }
  }
  throw lastError;
}

function removeTrailingCommas(content: string) {
  return content.replace(/,\s*([}\]])/g, "$1");
}

function repairMissingCommas(content: string) {
  return content
    .replace(/}\s*{/g, "},{")
    .replace(/]\s*{/g, "],{")
    .replace(/([}\]"])\s*\n\s*("[A-Za-z0-9_]+":)/g, "$1,\n$2")
    .replace(/(")\s*\n\s*(")(?=[^:\n]*[,}\]])/g, "$1,\n$2");
}

function normalizeBrief(
  value: Record<string, unknown>,
  meta: Pick<ThesisBrief, "mode" | "query"> & Pick<Partial<ThesisBrief>, "model" | "usage">,
): ThesisBrief {
  const candidates = readArray(value, "candidates")
    .map(normalizeCandidate)
    .filter((candidate): candidate is ThesisCandidate => Boolean(candidate))
    .slice(0, 6);

  return {
    mode: meta.mode,
    model: meta.model,
    query: meta.query,
    generatedAt: new Date().toISOString(),
    queryIntent: readString(value, "queryIntent", "query_intent") || "osrs_ge_research",
    thesis: readString(value, "thesis") || "No thesis returned.",
    candidates,
    rejectedTraps: readStringArray(value, "rejectedTraps", "rejected_traps").slice(0, 6),
    nextCliCommands: readStringArray(value, "nextCliCommands", "next_cli_commands").slice(0, 6),
    caveats: readStringArray(value, "caveats").slice(0, 6),
    usage: meta.usage,
  };
}

function normalizeCandidate(value: unknown): ThesisCandidate | null {
  if (!isObject(value)) return null;
  const item = readString(value, "item");
  if (!item) return null;
  return {
    item,
    setup: readString(value, "setup") || "GE research setup",
    evidence: readString(value, "evidence") || "Supplied evidence row",
    risk: readString(value, "risk") || "Market conditions can change before execution.",
    followUpCommand: readString(value, "followUpCommand", "follow_up_command") || `osrs-ge price "${item}"`,
    watchRuleCommand: readString(value, "watchRuleCommand", "watch_rule_command") || undefined,
    confidence: readConfidence(value),
    sourceProbe: readString(value, "sourceProbe", "source_probe") || "deepseek",
  };
}

function readConfidence(value: Record<string, unknown>): ThesisCandidate["confidence"] {
  const raw = readString(value, "confidence").toLowerCase();
  if (raw === "low" || raw === "medium" || raw === "high") return raw;
  return "medium";
}

function readStringArray(value: Record<string, unknown>, ...keys: string[]) {
  return readArray(value, ...keys).map(String).filter(Boolean);
}

function readArray(value: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const candidate = value[key];
    if (Array.isArray(candidate)) return candidate;
  }
  return [];
}

function readString(value: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const candidate = value[key];
    if (typeof candidate === "string") return candidate.trim();
    if (typeof candidate === "number") return String(candidate);
  }
  return "";
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function validateGeneratedCommands(brief: ThesisBrief) {
  const commands = [
    ...brief.nextCliCommands,
    ...brief.candidates.flatMap((candidate) => [
      candidate.followUpCommand,
      candidate.watchRuleCommand || "",
    ]),
  ].filter(Boolean);
  for (const command of commands) {
    if (!command.startsWith("osrs-ge ")) {
      throw new Error(`Generated command is outside the CLI surface: ${command}`);
    }
    if (/[\n\r;|<>`$]/.test(command)) {
      throw new Error(`Generated command contains shell metacharacters: ${command}`);
    }
    const [, subcommand] = command.split(/\s+/, 2);
    if (!["agent", "backtest", "breakouts", "doctor", "movers", "opportunities", "patterns", "price", "range-bottom", "schema", "timeseries", "watch"].includes(subcommand)) {
      throw new Error(`Generated subcommand is not allowlisted: ${subcommand}`);
    }
  }
}

function deriveIntent(query: string) {
  const q = query.toLowerCase();
  if (q.includes("breakout") || q.includes("bullish") || q.includes("limit buy") || q.includes("limit sell")) return "bullish_breakout";
  if (q.includes("vwap") || q.includes("range")) return "range_bottom";
  if (q.includes("dump") || q.includes("rebound")) return "shock_reversion";
  if (q.includes("spike") || q.includes("mover")) return "volume_shift";
  return "broad_ge_research";
}

function pickRows(query: string, rows = fallbackEvidenceRows) {
  const q = query.toLowerCase();
  if (q.includes("spike") || q.includes("mover")) {
    return [...rows].sort((a, b) => Number(b.probe.includes("movers") || b.probe.includes("breakout")) - Number(a.probe.includes("movers") || a.probe.includes("breakout")));
  }
  if (q.includes("dump") || q.includes("rebound")) {
    return [...rows].sort((a, b) =>
      Number(b.item.toLowerCase() === "bronze knife") - Number(a.item.toLowerCase() === "bronze knife") ||
      Number(b.probe.includes("patterns")) - Number(a.probe.includes("patterns")) ||
      b.score - a.score);
  }
  if (q.includes("breakout") || q.includes("bullish") || q.includes("lava") || q.includes("limit buy") || q.includes("sell")) {
    return [...rows].sort((a, b) => Number(b.probe.includes("breakout")) - Number(a.probe.includes("breakout")) || b.score - a.score);
  }
  return rows;
}

function nextCommandsForIntent(query: string, intent: string) {
  if (intent === "shock_reversion") {
    return shockReversionCommands(query);
  }
  return [
    `osrs-ge agent run "${query.replaceAll('"', "'")}" --json`,
    "osrs-ge doctor",
    "osrs-ge range-bottom --cash 40m --days 90 --step 6h",
  ];
}

function systemPrompt() {
  return `You are the scoped GE Thesis compiler for OSRS Grand Exchange research.
Return strict JSON only. Do not write markdown.
Use double-quoted JSON strings only; do not return comments, trailing commas, or extra wrapper text.
Use only the supplied CLI evidence rows. Do not invent prices, volumes, items, APIs, or commands.
For shock_reversion prompts, use Bronze knife as the reference row when supplied and make the patterns command the primary search command.
Stay inside OSRS GE market research. Do not provide gameplay automation, client automation, RWT, gambling, credentials, scraping, or arbitrary shell output.
Generated commands must start with osrs-ge and must stay inside the research CLI.`;
}

function userPrompt(query: string, rows: EvidenceRow[]) {
  const intent = deriveIntent(query);
  const evidenceLimit = intent === "shock_reversion" ? 4 : 8;
  return JSON.stringify({
    query,
    queryIntent: intent,
    evidenceRows: rows.slice(0, evidenceLimit),
    preferredCommands: nextCommandsForIntent(query, intent),
    candidateOrder: intent === "shock_reversion" ? rows.slice(0, 3).map((row) => row.item) : undefined,
    shockReversionRules: intent === "shock_reversion"
      ? [
          "Return exactly the first three supplied evidence rows as candidates, in order.",
          "The first candidate must be Bronze knife when supplied.",
          "Use each row followUpCommand exactly when supplied.",
        ]
      : undefined,
    requiredShape: {
      queryIntent: "short intent label",
      thesis: "one concise evidence-backed paragraph",
      candidates: [
        {
          item: "item name",
          setup: "setup type",
          evidence: "specific evidence from supplied rows",
          risk: "main invalidation",
          followUpCommand: "osrs-ge timeseries \"item name\" --step 1h --limit 168",
          watchRuleCommand: "osrs-ge watch add \"item name\" --min-volume 100000",
          confidence: "low|medium|high",
          sourceProbe: "probe name",
        },
      ],
      rejectedTraps: ["trap to avoid"],
      nextCliCommands: ["osrs-ge doctor"],
      caveats: ["public aggregate data cannot prove fills"],
    },
  });
}
