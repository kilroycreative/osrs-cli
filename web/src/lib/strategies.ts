export type ActiveStrategy = {
  id: string;
  title: string;
  item: string;
  setup: string;
  thesis: string;
  catalyst: string;
  risk: string;
  edgeScore: number;
  heat: number;
  confidence: "low" | "medium" | "high";
  status: "live" | "watch" | "cooling";
  prompt: string;
  command: string;
  watchCommand: string;
  tags: string[];
  limitBuy: string;
  limitSell: string;
  stretchSell: string;
  invalidation: string;
  trend30d: string;
  trend90d: string;
  distanceFromHigh: string;
  volume30d: string;
  turnover30d: string;
  buyLimit: string;
  liveMargin: string;
  liveROI: string;
  buyLimitProfit: string;
  briefPnl: string;
  briefROI: string;
  briefLimitProfit: string;
  liveSpread: string;
  liveSpreadROI: string;
  breakEvenSell: string;
  activeInvalidation: string;
  invalidationReason: string;
  taxDrag: string;
  capitalRequired: string;
  capitalRequiredValue: number;
  gpPer4h: string;
  gpPerDayMax: string;
  scoreInputs: string;
  scoreTrend: string;
  scoreLiquidity: string;
  scoreScale: string;
  mathLines: Array<{ label: string; value: string }>;
  dataFreshness: string;
};

export type StrategyDrop = {
  generatedAt: string;
  cycleId: number;
  cycleLabel: string;
  nextRefreshAt: string;
  staleAt: string;
  stale: boolean;
  headline: string;
  streak: string;
  source: "live-osrs-wiki" | "fallback";
  strategies: ActiveStrategy[];
  scoreboard: Array<{
    label: string;
    value: string;
    tone: "green" | "gold" | "red" | "blue";
  }>;
};

type LatestResponse = {
  data: Record<string, { high?: number; low?: number; highTime?: number; lowTime?: number }>;
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

type BreakoutCandidate = {
  id: number;
  name: string;
  buyLimit: number;
  lane: "bones" | "consumable" | "gear" | "ammo" | "resource";
};

const cycleMs = 6 * 60 * 60 * 1000;
const geWindowsPerDay = 6;
const defaultCapital = 40_000_000;
const apiBase = "https://prices.runescape.wiki/api/v1/osrs";
const wikiHeaders = {
  "User-Agent": "pp-osrs-ge-web/0.1 (+https://github.com/kilroycreative/pp-osrs-ge)",
  Accept: "application/json",
};

const breakoutUniverse: BreakoutCandidate[] = [
  { id: 11943, name: "Lava dragon bones", buyLimit: 7500, lane: "bones" },
  { id: 536, name: "Dragon bones", buyLimit: 7500, lane: "bones" },
  { id: 22124, name: "Superior dragon bones", buyLimit: 7500, lane: "bones" },
  { id: 31729, name: "Frost dragon bones", buyLimit: 7500, lane: "bones" },
  { id: 24777, name: "Blood shard", buyLimit: 8, lane: "gear" },
  { id: 11212, name: "Dragon arrow", buyLimit: 11000, lane: "ammo" },
  { id: 11230, name: "Dragon dart", buyLimit: 11000, lane: "ammo" },
  { id: 12934, name: "Zulrah's scales", buyLimit: 30000, lane: "resource" },
  { id: 6685, name: "Saradomin brew(4)", buyLimit: 2000, lane: "consumable" },
  { id: 2434, name: "Prayer potion(4)", buyLimit: 2000, lane: "consumable" },
  { id: 4151, name: "Abyssal whip", buyLimit: 70, lane: "gear" },
  { id: 13231, name: "Primordial crystal", buyLimit: 15, lane: "gear" },
];

const taxExemptItemIDs = new Set([
  13190,
  1755,
  5325,
  11090,
  2347,
  1733,
  233,
  5341,
  8794,
  5329,
  5343,
  1735,
  952,
  5331,
]);

export async function buildStrategyDrop(now = new Date()): Promise<StrategyDrop> {
  const current = now.getTime();
  const cycleStart = Math.floor(current / cycleMs) * cycleMs;
  const cycleId = Math.floor(cycleStart / cycleMs);
  const nextRefresh = cycleStart + cycleMs;

  try {
    const live = await buildLiveBreakoutStrategies(cycleId);
    return assembleDrop({
      now,
      cycleId,
      cycleStart,
      nextRefresh,
      source: "live-osrs-wiki",
      strategies: live,
    });
  } catch {
    return assembleDrop({
      now,
      cycleId,
      cycleStart,
      nextRefresh,
      source: "fallback",
      strategies: fallbackBreakoutStrategies(cycleId),
    });
  }
}

async function buildLiveBreakoutStrategies(cycleId: number): Promise<ActiveStrategy[]> {
  const latestPromise = fetchJSON<LatestResponse>("latest", 60);
  const series = await Promise.all(
    breakoutUniverse.map(async (candidate) => {
      const response = await fetchJSON<TimeseriesResponse>(`timeseries?id=${candidate.id}&timestep=24h`, cycleMs / 1000);
      return { candidate, points: response.data.filter(hasMid).slice(-100) };
    }),
  );
  const latest = await latestPromise;

  const scored = series
    .map(({ candidate, points }) => analyzeBreakout(candidate, points, latest.data[String(candidate.id)], cycleId))
    .filter((strategy): strategy is ActiveStrategy & { score: number } => Boolean(strategy))
    .sort((a, b) => b.score - a.score);

  const primary = scored.filter((strategy) => Number.parseFloat(strategy.trend90d) > 6);
  const selected = (primary.length >= 4 ? primary : scored).slice(0, 4);
  if (selected.length === 0) {
    throw new Error("No live breakout candidates");
  }

  return selected.map((strategy, index) => {
    const { score: _score, ...rest } = strategy;
    return {
      ...rest,
      status: index === 0 ? "live" : index === 1 ? "watch" : "cooling",
    };
  });
}

function analyzeBreakout(
  candidate: BreakoutCandidate,
  points: TimeseriesResponse["data"],
  latest?: LatestResponse["data"][string],
  cycleId = 0,
): (ActiveStrategy & { score: number }) | null {
  if (points.length < 28) return null;

  const mids = points.map(mid).filter((value): value is number => value !== null);
  const lastMid = liveMid(latest) || mids[mids.length - 1];
  const mid30 = mids[Math.max(0, mids.length - 31)];
  const mid90 = mids[Math.max(0, mids.length - 91)];
  const high90 = Math.max(...mids.slice(-90));
  const trend30 = pctChange(lastMid, mid30);
  const trend90 = pctChange(lastMid, mid90);
  const distanceFromHigh = Math.max(0, pctChange(high90, lastMid));
  const avgVolume30 = average(points.slice(-30).map(volume));
  const turnover30 = avgVolume30 * lastMid;

  const bullishEnough = (trend30 > 4 && trend90 > 7) || (trend90 > 14 && distanceFromHigh < 16);
  const liquidEnough = turnover30 > 40_000_000 || avgVolume30 > 15_000;
  if (!bullishEnough || !liquidEnough) return null;

  const currentLow = latest?.low || Math.floor(lastMid * 0.985);
  const currentHigh = latest?.high || Math.ceil(lastMid * 1.015);
  const liveTax = tax(candidate.id, currentHigh);
  const liveNetMargin = currentHigh - currentLow - liveTax;
  const liveROI = currentLow > 0 ? liveNetMargin / currentLow : 0;
  const entryHigh = Math.max(1, Math.floor(currentLow * 1.004));
  const entryLow = Math.max(1, Math.floor(entryHigh * 0.982));
  const firstSell = Math.max(currentHigh + 1, Math.ceil(Math.max(high90, lastMid) * 1.012));
  const stretchSell = Math.ceil(Math.max(high90, firstSell) * (distanceFromHigh <= 2 ? 1.035 : 1.022));
  const technicalInvalidation = Math.max(1, Math.floor(lastMid * 0.94));
  const briefTax = tax(candidate.id, firstSell);
  const briefNet = firstSell - briefTax - entryHigh;
  const briefROI = entryHigh > 0 ? briefNet / entryHigh : 0;
  const briefLimitProfit = briefNet * candidate.buyLimit;
  const breakEven = breakEvenSell(candidate.id, entryHigh);
  const activeInvalidation = Math.max(technicalInvalidation, breakEven);
  const invalidationReason =
    activeInvalidation === breakEven
      ? `break-even is active because ${gp(breakEven)} is above the technical invalidation ${gp(technicalInvalidation)}`
      : `technical invalidation is active because ${gp(technicalInvalidation)} is above break-even ${gp(breakEven)}`;
  const capitalRequired = candidate.buyLimit * entryHigh;
  const latestTrade = Math.max(latest?.highTime || 0, latest?.lowTime || 0);
  const freshness = latestTrade > 0 ? Math.max(0.35, 1 - Math.max(0, Math.floor(Date.now() / 1000) - latestTrade) / 7200) : 0.65;
  const scoreTrend = Math.max(briefNet, 1) * Math.max(briefROI, 0.0001) * freshness;
  const scoreLiquidity = Math.log10(Math.max(avgVolume30, 1) + 10) * Math.min(5, avgVolume30 / 1000) * (1 + laneBoost(candidate.lane) / 10);
  const scoreScale = Math.log1p(candidate.buyLimit);
  const score = scoreTrend * scoreLiquidity * scoreScale;
  const edgeScore = Math.max(1, Math.min(99, Math.round(score / 75)));
  const statusTags = ["breakout", "30d+", briefNet > 0 ? "tax-ok" : "invalidated"];

  return {
    id: `breakout-${candidate.id}`,
    title: `${candidate.name} breakout`,
    item: candidate.name,
    setup: "1-3 month bullish breakout",
    thesis: `${candidate.name} is pressing near its 90-day high after a ${formatPct(trend30)} 30-day move and ${formatPct(trend90)} 90-day move, with enough gp turnover to justify an active limit ladder.`,
    catalyst: `${formatShortNumber(avgVolume30)}/day 30-day volume, ${formatShortNumber(turnover30)} gp/day turnover, ${formatGapPct(distanceFromHigh)} below the 90-day high.`,
    risk: "Breakout entries fail quickly if the bid falls back below the invalidation band or volume dries up.",
    edgeScore,
    heat: Math.max(1, Math.min(99, edgeScore + (cycleId % 3))),
    confidence: edgeScore >= 86 ? "high" : edgeScore >= 74 ? "medium" : "low",
    status: "cooling",
    prompt: `${candidate.name} bullish breakout over the last 1-3 months with high value volume; give limit buy and sell zones`,
    command: `osrs-ge breakouts --limit 8 --min-turnover 40m --json`,
    watchCommand: `osrs-ge watch add "${candidate.name}" --below ${entryHigh} --min-volume ${Math.max(1000, Math.round(avgVolume30 * 0.25))}`,
    tags: statusTags,
    limitBuy: gpRange(entryLow, entryHigh),
    limitSell: gp(firstSell),
    stretchSell: gp(stretchSell),
    invalidation: `below ${gp(activeInvalidation)}`,
    trend30d: formatPct(trend30),
    trend90d: formatPct(trend90),
    distanceFromHigh: formatGapPct(distanceFromHigh),
    volume30d: `${formatShortNumber(avgVolume30)}/day`,
    turnover30d: `${formatShortNumber(turnover30)} gp/day`,
    buyLimit: `${formatShortNumber(candidate.buyLimit)} limit`,
    liveMargin: `${gp(liveNetMargin)} net`,
    liveROI: `${formatPct(liveROI * 100)} ROI`,
    buyLimitProfit: `${gp(briefLimitProfit)} / limit`,
    briefPnl: `${gp(briefNet)} net`,
    briefROI: `${formatPct(briefROI * 100)} ROI`,
    briefLimitProfit: `${gp(briefLimitProfit)} / limit`,
    liveSpread: `${gp(liveNetMargin)} net`,
    liveSpreadROI: `${formatPct(liveROI * 100)} ROI`,
    breakEvenSell: gp(breakEven),
    activeInvalidation: `below ${gp(activeInvalidation)}`,
    invalidationReason,
    taxDrag: `-${gp(briefTax)}/unit · -${gp(briefTax * candidate.buyLimit)}/limit`,
    capitalRequired: `${gp(capitalRequired)} to fill`,
    capitalRequiredValue: capitalRequired,
    gpPer4h: `${gp(briefLimitProfit)} / 4h`,
    gpPerDayMax: `${gp(briefLimitProfit * geWindowsPerDay)} / day max`,
    scoreInputs: "trend+liq+scale",
    scoreTrend: scoreTrend.toFixed(2),
    scoreLiquidity: scoreLiquidity.toFixed(2),
    scoreScale: scoreScale.toFixed(2),
    mathLines: [
      { label: "Brief P&L", value: `${gp(firstSell)} sell - ${gp(entryHigh)} buy - ${gp(briefTax)} tax = ${gp(briefNet)}` },
      { label: "Brief ROI", value: `${gp(briefNet)} / ${gp(entryHigh)} = ${formatPct(briefROI * 100)}` },
      { label: "Live spread", value: `${gp(currentHigh)} high - ${gp(currentLow)} low - ${gp(liveTax)} tax = ${gp(liveNetMargin)}` },
      { label: "Break-even sell", value: `${gp(breakEven)} recovers ${gp(entryHigh)} after tax` },
      { label: "Tax drag", value: `${gp(briefTax)} per unit; ${gp(briefTax * candidate.buyLimit)} per ${formatShortNumber(candidate.buyLimit)} limit` },
      { label: "Capital required", value: `${formatShortNumber(candidate.buyLimit)} limit * ${gp(entryHigh)} = ${gp(capitalRequired)}` },
      { label: "Score", value: `${scoreTrend.toFixed(2)} trend * ${scoreLiquidity.toFixed(2)} liq * ${scoreScale.toFixed(2)} scale = ${score.toFixed(2)}` },
    ],
    dataFreshness: latestTrade > 0 ? `${formatAgeSeconds(Math.max(0, Math.floor(Date.now() / 1000) - latestTrade))} old` : "latest age n/a",
    score,
  };
}

function assembleDrop({
  now,
  cycleId,
  cycleStart,
  nextRefresh,
  source,
  strategies,
}: {
  now: Date;
  cycleId: number;
  cycleStart: number;
  nextRefresh: number;
  source: StrategyDrop["source"];
  strategies: ActiveStrategy[];
}): StrategyDrop {
  return {
    generatedAt: now.toISOString(),
    cycleId,
    cycleLabel: cycleLabel(cycleStart),
    nextRefreshAt: new Date(nextRefresh).toISOString(),
    staleAt: new Date(nextRefresh).toISOString(),
    stale: now.getTime() >= nextRefresh,
    headline: strategies[0]?.title || "Breakout watch",
    streak: `${2 + (cycleId % 6)} breakout cycles`,
    source,
    strategies,
    scoreboard: [
      { label: "Breakouts", value: String(strategies.length), tone: "green" },
      { label: "Refresh", value: "6h", tone: "blue" },
      { label: "Score uses", value: "trend+liq+scale", tone: "gold" },
      { label: "Reachable", value: `${strategies.filter((strategy) => strategy.capitalRequiredValue <= defaultCapital).length}/${strategies.length}`, tone: "blue" },
    ],
  };
}

function fallbackBreakoutStrategies(cycleId: number): ActiveStrategy[] {
  const rows: ActiveStrategy[] = [
    {
      id: "fallback-lava-dragon-bones",
      title: "Lava dragon bones breakout",
      item: "Lava dragon bones",
      setup: "1-3 month bullish breakout",
      thesis: "Lava dragon bones are the reference breakout candidate: high-value bones with strong 30/90-day momentum and enough daily turnover to ladder entries.",
      catalyst: "Fallback row used only if the live OSRS Wiki API is unavailable.",
      risk: "Do not chase above the sell band; wait for bid support.",
      edgeScore: 88,
      heat: 90 + (cycleId % 4),
      confidence: "high",
      status: "live",
      prompt: "Lava dragon bones bullish breakout over the last 1-3 months with high value volume; give limit buy and sell zones",
      command: 'osrs-ge timeseries "Lava dragon bones" --step 24h --limit 90',
      watchCommand: 'osrs-ge watch add "Lava dragon bones" --min-volume 1000',
      tags: ["breakout", "bones", "fallback"],
      limitBuy: "live API required",
      limitSell: "live API required",
      stretchSell: "live API required",
      invalidation: "live API required",
      trend30d: "live API",
      trend90d: "live API",
      distanceFromHigh: "live API",
      volume30d: "live API",
      turnover30d: "live API",
      buyLimit: "7.5k limit",
      liveMargin: "live API",
      liveROI: "live API",
      buyLimitProfit: "live API",
      briefPnl: "live API",
      briefROI: "live API",
      briefLimitProfit: "live API",
      liveSpread: "live API",
      liveSpreadROI: "live API",
      breakEvenSell: "live API",
      activeInvalidation: "live API",
      invalidationReason: "live API required to compare technical invalidation against post-tax break-even",
      taxDrag: "live API",
      capitalRequired: "live API",
      capitalRequiredValue: Number.POSITIVE_INFINITY,
      gpPer4h: "live API",
      gpPerDayMax: "live API theoretical max",
      scoreInputs: "trend+liq+scale",
      scoreTrend: "live API",
      scoreLiquidity: "live API",
      scoreScale: "8.92",
      mathLines: [
        { label: "Data", value: "Fallback row only; live OSRS Wiki data is required for the full computation." },
      ],
      dataFreshness: "live API",
    },
  ];
  return rows;
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

function hasMid(point: TimeseriesResponse["data"][number]) {
  return mid(point) !== null;
}

function mid(point?: TimeseriesResponse["data"][number]) {
  if (!point) return null;
  const prices = [point.avgHighPrice, point.avgLowPrice].filter((value): value is number => typeof value === "number" && value > 0);
  if (prices.length === 0) return null;
  return average(prices);
}

function liveMid(point?: LatestResponse["data"][string]) {
  if (!point) return null;
  const prices = [point.high, point.low].filter((value): value is number => typeof value === "number" && value > 0);
  if (prices.length === 0) return null;
  return average(prices);
}

function volume(point: TimeseriesResponse["data"][number]) {
  return (point.highPriceVolume || 0) + (point.lowPriceVolume || 0);
}

function average(values: number[]) {
  return values.reduce((sum, value) => sum + value, 0) / Math.max(values.length, 1);
}

function pctChange(current: number, previous: number) {
  if (!previous) return 0;
  return ((current - previous) / previous) * 100;
}

function tax(itemID: number, price: number) {
  if (taxExemptItemIDs.has(itemID) || price <= 100) return 0;
  return Math.min(Math.floor(price * 0.02), 5_000_000);
}

function breakEvenSell(itemID: number, buyPrice: number) {
  if (taxExemptItemIDs.has(itemID) || buyPrice <= 100) return buyPrice;
  let sellPrice = Math.ceil(buyPrice / 0.98);
  while (sellPrice - tax(itemID, sellPrice) < buyPrice) {
    sellPrice += 1;
  }
  return sellPrice;
}

function laneBoost(lane: BreakoutCandidate["lane"]) {
  if (lane === "bones") return 10;
  if (lane === "gear") return 6;
  if (lane === "ammo") return 4;
  return 2;
}

function gp(value: number) {
  return `${Math.round(value).toLocaleString("en-US")} gp`;
}

function gpRange(low: number, high: number) {
  return `${Math.round(low).toLocaleString("en-US")}-${Math.round(high).toLocaleString("en-US")} gp`;
}

function formatPct(value: number) {
  const sign = value > 0 ? "+" : "";
  return `${sign}${value.toFixed(1)}%`;
}

function formatGapPct(value: number) {
  return `${Math.max(0, value).toFixed(1)}%`;
}

function formatShortNumber(value: number) {
  const sign = value < 0 ? "-" : "";
  const abs = Math.abs(value);
  if (abs >= 1_000_000_000) return `${sign}${(abs / 1_000_000_000).toFixed(1)}b`;
  if (abs >= 1_000_000) return `${sign}${(abs / 1_000_000).toFixed(1)}m`;
  if (abs >= 1_000) return `${sign}${(abs / 1_000).toFixed(1)}k`;
  return `${Math.round(value)}`;
}

function formatAgeSeconds(seconds: number) {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
  return `${Math.round(seconds / 86400)}d`;
}

function cycleLabel(cycleStart: number) {
  const start = new Date(cycleStart);
  return `${start.toISOString().slice(11, 16)} UTC breakout drop`;
}
