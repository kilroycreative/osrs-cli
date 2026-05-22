import type { EvidenceRow } from "@/lib/brief";

type ShockCandidate = {
  id: number;
  name: string;
  buyLimit: number;
  reference?: boolean;
};

type TimeseriesResponse = {
  data: Array<{
    timestamp: number;
    avgHighPrice?: number;
    avgLowPrice?: number;
    highPriceVolume?: number;
    lowPriceVolume?: number;
  }>;
};

type PatternPoint = {
  timestamp: number;
  high: number | null;
  low: number | null;
  highVolume: number;
  lowVolume: number;
};

const apiBase = "https://prices.runescape.wiki/api/v1/osrs";
const wikiHeaders = {
  "User-Agent": "pp-osrs-ge-web/0.1 (+https://github.com/kilroycreative/pp-osrs-ge)",
  Accept: "application/json",
};

const shockReversionCommand = 'osrs-ge patterns --like "Bronze knife" --cash 40m --days 7 --step 5m --limit 15 --json';

const shockUniverse: ShockCandidate[] = [
  { id: 864, name: "Bronze knife", buyLimit: 7000, reference: true },
  { id: 9375, name: "Bronze bolts (unf)", buyLimit: 13000 },
  { id: 31967, name: "Oak repair kit", buyLimit: 13000 },
  { id: 31708, name: "Crab paste", buyLimit: 13000 },
  { id: 6020, name: "Leaves", buyLimit: 13000 },
  { id: 32309, name: "Raw giant krill", buyLimit: 15000 },
  { id: 6814, name: "Fur", buyLimit: 18000 },
  { id: 22254, name: "Willow shield", buyLimit: 18000 },
  { id: 6701, name: "Baked potato", buyLimit: 13000 },
  { id: 958, name: "Grey wolf fur", buyLimit: 18000 },
  { id: 22257, name: "Maple shield", buyLimit: 18000 },
  { id: 8778, name: "Oak plank", buyLimit: 13000 },
];

export async function buildShockReversionRows(): Promise<EvidenceRow[]> {
  const analyzed = await Promise.all(
    shockUniverse.map(async (candidate) => {
      try {
        const response = await fetchJSON<TimeseriesResponse>(`timeseries?id=${candidate.id}&timestep=5m`, 300);
        return analyzeShockCandidate(candidate, response.data);
      } catch {
        return null;
      }
    }),
  );

  const rows = analyzed.filter((row): row is EvidenceRow => Boolean(row));
  const reference = rows.find((row) => row.item === "Bronze knife") || bronzeKnifeFallbackRow();
  const analogs = rows
    .filter((row) => row.item !== "Bronze knife")
    .sort((a, b) => b.score - a.score);

  return [reference, ...analogs].slice(0, 8);
}

export function fallbackShockReversionRows(): EvidenceRow[] {
  return [
    bronzeKnifeFallbackRow(),
    {
      probe: "patterns-bronze-knife",
      item: "Bronze bolts (unf)",
      metric: "18 -> 99 gp analog",
      evidence:
        "Bronze-knife-family seed: cheap unit price, 13k buy limit, previous low-to-high shock behavior, and enough event volume to test with the patterns scan.",
      setup: "Cheap high-limit shock analog",
      score: 84,
      followUpCommand: shockReversionCommand,
      watchRuleCommand: 'osrs-ge watch add "Bronze bolts (unf)" --min-volume 1000',
    },
    {
      probe: "patterns-bronze-knife",
      item: "Oak repair kit",
      metric: "41 -> 227 gp analog",
      evidence:
        "Bronze-knife-family seed from recent pattern output: low unit price, 13k buy limit, high event ratio, and multi-million gp theoretical limit-cycle edge.",
      setup: "Cheap high-limit shock analog",
      score: 82,
      followUpCommand: shockReversionCommand,
      watchRuleCommand: 'osrs-ge watch add "Oak repair kit" --min-volume 1000',
    },
    {
      probe: "patterns-bronze-knife",
      item: "Raw giant krill",
      metric: "13 -> 88 gp analog",
      evidence:
        "Bronze-knife-family seed: cheap low print, high buy limit, visible rebound print, and event volume on both legs.",
      setup: "Cheap high-limit shock analog",
      score: 78,
      followUpCommand: shockReversionCommand,
      watchRuleCommand: 'osrs-ge watch add "Raw giant krill" --min-volume 1000',
    },
  ];
}

export function shockReversionCommands(query: string) {
  return [
    shockReversionCommand,
    `osrs-ge agent run "${query.replaceAll('"', "'")}" --json`,
    'osrs-ge price "Bronze knife"',
    'osrs-ge timeseries "Bronze knife" --step 5m --limit 2016',
  ];
}

function analyzeShockCandidate(candidate: ShockCandidate, rawPoints: TimeseriesResponse["data"]): EvidenceRow | null {
  const cutoff = Math.floor(Date.now() / 1000) - 7 * 24 * 60 * 60;
  const points = rawPoints
    .filter((point) => point.timestamp >= cutoff)
    .map((point): PatternPoint => ({
      timestamp: point.timestamp,
      high: positive(point.avgHighPrice),
      low: positive(point.avgLowPrice),
      highVolume: point.highPriceVolume || 0,
      lowVolume: point.lowPriceVolume || 0,
    }))
    .sort((a, b) => a.timestamp - b.timestamp);

  if (points.length === 0) return candidate.reference ? bronzeKnifeFallbackRow() : null;

  const best = bestDumpRebound(points);
  if (!best) return candidate.reference ? bronzeKnifeFallbackRow() : null;

  const tax = geTax(best.high);
  const net = best.high - best.low - tax;
  const roi = best.low > 0 ? net / best.low : 0;
  const limitProfit = net * candidate.buyLimit;
  const reboundHours = (best.highTime - best.lowTime) / 3600;
  const volumeFloor = Math.min(best.lowVolume, best.highVolume);
  const setup = candidate.reference ? "Bronze-knife reference" : classifySetup(net, roi, best.ratio);
  const score =
    (candidate.reference ? 92 : 0) +
    Math.max(0, net) * Math.max(roi, 0.05) * Math.log10(Math.max(volumeFloor, 10)) * Math.log10(candidate.buyLimit);

  return {
    probe: "patterns-bronze-knife",
    item: candidate.name,
    metric: `${gp(best.low)} -> ${gp(best.high)} gp`,
    evidence: [
      `dip ${gp(best.low)} gp to rebound ${gp(best.high)} gp in ${formatHours(reboundHours)}`,
      `tax-adjusted ${gp(net)} gp/ea, ${formatPct(roi)} ROI`,
      `dip volume ${short(best.lowVolume)}, rebound volume ${short(best.highVolume)}`,
      `${short(candidate.buyLimit)} buy limit, ${gp(limitProfit)} gp theoretical limit edge`,
    ].join("; "),
    setup,
    score,
    followUpCommand: shockReversionCommand,
    watchRuleCommand: `osrs-ge watch add "${candidate.name}" --min-volume ${Math.max(300, Math.round(volumeFloor * 0.5))}`,
  };
}

function bestDumpRebound(points: PatternPoint[]) {
  let best:
    | {
        low: number;
        high: number;
        ratio: number;
        lowVolume: number;
        highVolume: number;
        lowTime: number;
        highTime: number;
        score: number;
      }
    | null = null;

  for (let lowIndex = 0; lowIndex < points.length; lowIndex++) {
    const lowPoint = points[lowIndex];
    if (!lowPoint.low || lowPoint.lowVolume <= 0) continue;

    for (let highIndex = lowIndex; highIndex < points.length; highIndex++) {
      const highPoint = points[highIndex];
      if (!highPoint.high || highPoint.highVolume <= 0) continue;
      const reboundSeconds = highPoint.timestamp - lowPoint.timestamp;
      if (reboundSeconds < 0 || reboundSeconds > 7 * 24 * 60 * 60) continue;

      const ratio = highPoint.high / lowPoint.low;
      const net = highPoint.high - lowPoint.low - geTax(highPoint.high);
      const score = Math.max(0, net) * Math.max(ratio - 1, 0.05) * Math.log1p(Math.min(lowPoint.lowVolume, highPoint.highVolume));
      if (!best || score > best.score) {
        best = {
          low: lowPoint.low,
          high: highPoint.high,
          ratio,
          lowVolume: lowPoint.lowVolume,
          highVolume: highPoint.highVolume,
          lowTime: lowPoint.timestamp,
          highTime: highPoint.timestamp,
          score,
        };
      }
    }
  }

  return best;
}

async function fetchJSON<T>(path: string, revalidateSeconds: number): Promise<T> {
  const response = await fetch(`${apiBase}/${path}`, {
    headers: wikiHeaders,
    next: { revalidate: revalidateSeconds },
  });
  if (!response.ok) {
    throw new Error(`OSRS Wiki ${path} HTTP ${response.status}`);
  }
  return response.json() as Promise<T>;
}

function bronzeKnifeFallbackRow(): EvidenceRow {
  return {
    probe: "patterns-bronze-knife",
    item: "Bronze knife",
    metric: "10 -> 12 gp live reference",
    evidence:
      "Reference item for this query family: cheap unit price, 7k buy limit, current 10/12 gp spread, 2 gp net, 20.0% ROI, and 14,000 gp theoretical edge per full buy limit. Use this as the template seed, then scan similar cheap high-limit items.",
    setup: "Bronze-knife shock reversion reference",
    score: 92,
    followUpCommand: shockReversionCommand,
    watchRuleCommand: 'osrs-ge watch add "Bronze knife" --min-volume 300',
  };
}

function positive(value?: number) {
  return typeof value === "number" && value > 0 ? value : null;
}

function geTax(price: number) {
  return Math.min(Math.floor(price * 0.02), 5_000_000);
}

function classifySetup(net: number, roi: number, ratio: number) {
  if (net >= 50 && roi >= 1.5 && ratio >= 2) return "Fresh dump/rebound analog";
  if (net >= 20 && roi >= 0.4) return "Cheap high-limit shock analog";
  return "Bronze-knife-family watch";
}

function gp(value: number) {
  return Math.round(value).toLocaleString("en-US");
}

function short(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}m`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return Math.round(value).toLocaleString("en-US");
}

function formatPct(value: number) {
  return `${(value * 100).toFixed(1)}%`;
}

function formatHours(value: number) {
  if (value < 1) return `${Math.round(value * 60)}m`;
  if (value < 48) return `${value.toFixed(1)}h`;
  return `${(value / 24).toFixed(1)}d`;
}
