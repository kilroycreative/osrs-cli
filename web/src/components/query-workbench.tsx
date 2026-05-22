"use client";

import {
  FormEvent,
  KeyboardEvent as ReactKeyboardEvent,
  PointerEvent as ReactPointerEvent,
  ReactNode,
  TouchEvent as ReactTouchEvent,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Activity,
  Bell,
  ChevronLeft,
  Check,
  ChevronRight,
  CircleAlert,
  Clock,
  Copy,
  Flame,
  Loader2,
  Send,
  ShieldCheck,
  Trophy,
  Zap,
} from "lucide-react";
import type { ThesisBrief } from "@/lib/brief";
import type { ActiveStrategy, StrategyDrop } from "@/lib/strategies";

const samples = [
  "items at the bottom of VWAP with consistent volume and 40m cash",
  "cheap high buy-limit items that dump and rebound within a week",
  "volume spikes today with tax-adjusted spread still intact",
];

type StrategyPreset = {
  id: string;
  category: string;
  name: string;
  description: string;
  prompt: string;
  command: string;
  capital: string;
  scale: "micro" | "mid" | "macro";
};

const strategyPresets: StrategyPreset[] = [
  {
    id: "emerald-amulet",
    category: "Margin flips",
    name: "Emerald amulet flip",
    description: "Small-stack jewelry spread with high repeatability.",
    prompt: "Show tax-adjusted buy and sell zones for emerald amulets with a micro stack.",
    command: 'osrs-ge price "emerald amulet" --capital 1m --explain',
    capital: "50k-1m",
    scale: "micro",
  },
  {
    id: "prayer-potion",
    category: "Margin flips",
    name: "Prayer potion(4)",
    description: "Mid-stack consumable flip with reliable demand.",
    prompt: "Find the post-tax margin, break-even, and capital required for prayer potion(4).",
    command: 'osrs-ge price "prayer potion(4)" --capital 10m --explain',
    capital: "1m-10m",
    scale: "mid",
  },
  {
    id: "super-combat",
    category: "Margin flips",
    name: "Super combat(4)",
    description: "Higher-value potion spread for deeper stacks.",
    prompt: "Check super combat potion(4) for a tax-adjusted flip with scale and invalidation.",
    command: 'osrs-ge price "super combat potion(4)" --capital 20m --explain',
    capital: "10m-25m",
    scale: "mid",
  },
  {
    id: "anglerfish",
    category: "Margin flips",
    name: "Anglerfish",
    description: "Food flip screened by volume and per-limit profit.",
    prompt: "Scan anglerfish for post-tax spread, buy-limit profit, and break-even sell.",
    command: 'osrs-ge price "anglerfish" --capital 20m --explain',
    capital: "5m-20m",
    scale: "mid",
  },
  {
    id: "herb-cleaning",
    category: "Process arbitrage",
    name: "Grimy to clean herbs",
    description: "Compares herb cleaning input and output economics.",
    prompt: "Compare grimy ranarr weed to clean ranarr weed after tax and show per-limit economics.",
    command: 'osrs-ge price "grimy ranarr weed" --explain',
    capital: "1m-10m",
    scale: "mid",
  },
  {
    id: "dragon-scale-dust",
    category: "Process arbitrage",
    name: "Dragon scale dust",
    description: "Checks scale-to-dust processing after GE tax.",
    prompt: "Compare blue dragon scale to dragon scale dust after tax and show the processing spread.",
    command: 'osrs-ge price "blue dragon scale" --explain',
    capital: "1m-10m",
    scale: "mid",
  },
  {
    id: "dose-decanting",
    category: "Process arbitrage",
    name: "Dose decanting",
    description: "Tests potion dose conversion against the current spread.",
    prompt: "Compare prayer potion(3) and prayer potion(4) decanting economics after tax.",
    command: 'osrs-ge price "prayer potion(3)" --explain',
    capital: "1m-10m",
    scale: "mid",
  },
  {
    id: "unfinished-potions",
    category: "Process arbitrage",
    name: "Unfinished potions",
    description: "Checks herb to unfinished-potion conversion.",
    prompt: "Compare ranarr weed and ranarr potion (unf) after tax with capital required.",
    command: 'osrs-ge price "ranarr potion (unf)" --explain',
    capital: "1m-10m",
    scale: "mid",
  },
  {
    id: "breakout-scanner",
    category: "Swing breakouts",
    name: "Current breakout scanner",
    description: "Ranks 30/90-day structure with liquidity and scale.",
    prompt: "Find 1-3 month bullish breakouts with high value volume and post-tax sell zones.",
    command: "osrs-ge patterns --cash 40m --limit 15 --explain",
    capital: "10m+",
    scale: "macro",
  },
  {
    id: "gap-closers",
    category: "Swing breakouts",
    name: "90d gap closers",
    description: "Finds names reclaiming a larger range.",
    prompt: "Find 90-day gap closers with improving volume and reachable capital requirements.",
    command: "osrs-ge range-bottom --cash 40m --days 90 --step 6h --explain",
    capital: "10m+",
    scale: "macro",
  },
  {
    id: "dragon-bones-ladder",
    category: "Buy-limit grinds",
    name: "Dragon bones ladder",
    description: "High-limit bone grind with clear per-window economics.",
    prompt: "Build a dragon bones ladder with post-tax gp per 4h and break-even invalidation.",
    command: 'osrs-ge price "dragon bones" --capital 20m --explain',
    capital: "5m-20m",
    scale: "mid",
  },
  {
    id: "logs-grind",
    category: "Buy-limit grinds",
    name: "Magic and yew logs",
    description: "Compares log flips by capital required and per-limit profit.",
    prompt: "Compare magic logs and yew logs by post-tax gp per limit and reachable capital.",
    command: "osrs-ge opportunities --min-volume 1000 --sort limit-profit --capital 20m --explain",
    capital: "5m-20m",
    scale: "mid",
  },
  {
    id: "dmm-leagues",
    category: "Event-driven",
    name: "DMM / Leagues spike",
    description: "Looks for event-driven demand and short-term movers.",
    prompt: "Scan for DMM or Leagues demand spikes with post-tax spread still intact.",
    command: "osrs-ge movers --interval 1h --limit 25 --explain",
    capital: "10m+",
    scale: "macro",
  },
  {
    id: "bond-arbitrage",
    category: "Event-driven",
    name: "Bond arbitrage",
    description: "Tracks bond movement and notes tax exemption.",
    prompt: "Check old school bond arbitrage and explain the tax-exempt economics.",
    command: 'osrs-ge price "old school bond" --capital 100m --explain',
    capital: "50m+",
    scale: "macro",
  },
];

const presetCategories = Array.from(new Set(strategyPresets.map((preset) => preset.category)));

export function QueryWorkbench({ initialDrop }: { initialDrop: StrategyDrop }) {
  const firstPreset = strategyPresets[0];
  const [query, setQuery] = useState(initialDrop.strategies[0]?.prompt || firstPreset.prompt || samples[0]);
  const [capitalText, setCapitalText] = useState("40m");
  const [selectedPresetId, setSelectedPresetId] = useState(firstPreset.id);
  const [brief, setBrief] = useState<ThesisBrief | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [now, setNow] = useState(() => new Date(initialDrop.generatedAt).getTime());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, []);

  async function runQuery(nextQuery = query) {
    const trimmed = nextQuery.trim();
    if (!trimmed) return;
    setQuery(trimmed);
    setLoading(true);
    setError("");
    try {
      const response = await fetch("/api/brief", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ query: trimmed }),
      });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || "Unable to run thesis.");
      }
      setBrief(payload as ThesisBrief);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to run thesis.");
      setBrief(null);
    } finally {
      setLoading(false);
    }
  }

  async function submit(event?: FormEvent) {
    event?.preventDefault();
    await runQuery(query);
  }

  const activeBrief = useMemo(() => brief, [brief]);
  const freshness = useMemo(() => formatTimeLeft(initialDrop.nextRefreshAt, now), [initialDrop.nextRefreshAt, now]);
  const capitalValue = useMemo(() => parseCapital(capitalText), [capitalText]);
  const selectedPreset = strategyPresets.find((preset) => preset.id === selectedPresetId) || firstPreset;
  const reachableCount = initialDrop.strategies.filter((strategy) => strategy.capitalRequiredValue <= capitalValue).length;
  const scoreboard = [
    ...initialDrop.scoreboard.filter((score) => score.label !== "Reachable"),
    { label: "Reachable", value: `${reachableCount}/${initialDrop.strategies.length}`, tone: "blue" as const },
  ];

  function choosePreset(preset: StrategyPreset) {
    setSelectedPresetId(preset.id);
    setQuery(preset.prompt);
  }

  return (
    <div className="workbench-grid">
      <form className="query-panel" onSubmit={submit}>
        <div className="panel-header">
          <div>
            <p className="eyebrow">Priority lane</p>
            <h2>Plain-English scan</h2>
          </div>
          <ShieldCheck aria-hidden="true" size={22} />
        </div>

        <textarea
          aria-label="Market thesis"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          spellCheck={false}
        />

        <label className="capital-input">
          <span>Capital</span>
          <input
            aria-label="Playable capital"
            value={capitalText}
            onChange={(event) => setCapitalText(event.target.value)}
            placeholder="40m"
            inputMode="decimal"
          />
        </label>

        <div className="preset-library" aria-label="Strategy presets">
          {presetCategories.map((category) => (
            <div className="preset-group" key={category}>
              <span>{category}</span>
              {strategyPresets
                .filter((preset) => preset.category === category)
                .map((preset) => (
                  <button
                    type="button"
                    className={preset.id === selectedPreset.id ? "active" : ""}
                    key={preset.id}
                    onClick={() => choosePreset(preset)}
                    title={preset.description}
                  >
                    <ChevronRight size={14} aria-hidden="true" />
                    <strong>{preset.name}</strong>
                    <small>{preset.scale}</small>
                  </button>
                ))}
            </div>
          ))}
        </div>

        <div className="preset-preview">
          <strong>{selectedPreset.name}</strong>
          <span>{selectedPreset.description}</span>
          <code>{selectedPreset.command}</code>
          <small>{selectedPreset.capital} capital · {selectedPreset.scale} scale · auto-run off</small>
        </div>

        <button className="run-button" disabled={loading} type="submit">
          {loading ? <Loader2 className="spin" size={18} aria-hidden="true" /> : <Send size={18} aria-hidden="true" />}
          Run prompt
        </button>

        <div className="policy-grid" aria-label="Model policy">
          <span>DeepSeek scoped</span>
          <span>CLI commands only</span>
          <span>Reachable {reachableCount}/{initialDrop.strategies.length}</span>
          <span>Fresh drop {freshness}</span>
        </div>
      </form>

      <section className="result-panel" aria-label="Thesis result">
        {error ? (
          <div className="state-message error">
            <CircleAlert size={22} aria-hidden="true" />
            <span>{error}</span>
          </div>
        ) : activeBrief ? (
          <BriefView brief={activeBrief} query={query} />
        ) : (
          <ActiveDropView drop={initialDrop} freshness={freshness} loading={loading} onRun={runQuery} query={query} capitalValue={capitalValue} />
        )}
      </section>

      <aside className="alert-panel" aria-label="Saved scans">
        <div className="panel-header">
          <div>
            <p className="eyebrow">Cycle board</p>
            <h2>Active queue</h2>
          </div>
          <Bell size={22} aria-hidden="true" />
        </div>
        <div className="scoreboard">
          {scoreboard.map((score) => (
            <div className={`score ${score.tone}`} key={score.label}>
              <span>{score.label}</span>
              <strong>{score.value}</strong>
            </div>
          ))}
        </div>
        {initialDrop.strategies.slice(0, 4).map((strategy) => (
          <div className="alert-row" key={strategy.id}>
            <span>{strategy.item}</span>
            <strong>{strategy.status}</strong>
          </div>
        ))}
      </aside>
    </div>
  );
}

function ActiveDropView({
  drop,
  freshness,
  loading,
  onRun,
  query,
  capitalValue,
}: {
  drop: StrategyDrop;
  freshness: string;
  loading: boolean;
  onRun: (query: string) => Promise<void>;
  query: string;
  capitalValue: number;
}) {
  const leader = drop.strategies[0];
  const [activeIndex, setActiveIndex] = useState(0);
  const swipeStart = useRef<{ x: number; y: number } | null>(null);
  const activeStrategy = drop.strategies[activeIndex] || leader;
  const strategyCount = drop.strategies.length;

  function goToStrategy(index: number) {
    if (strategyCount === 0) return;
    setActiveIndex((index + strategyCount) % strategyCount);
  }

  function goBy(delta: number) {
    if (strategyCount === 0) return;
    setActiveIndex((current) => (current + delta + strategyCount) % strategyCount);
  }

  function finishSwipe(clientX: number, clientY: number) {
    const start = swipeStart.current;
    swipeStart.current = null;
    if (!start) return;

    const deltaX = clientX - start.x;
    const deltaY = clientY - start.y;
    const absX = Math.abs(deltaX);
    const absY = Math.abs(deltaY);
    if (absX < 44 || absX < absY * 1.2) return;

    goBy(deltaX < 0 ? 1 : -1);
  }

  function handlePointerDown(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.pointerType !== "mouse" || event.button !== 0) return;
    swipeStart.current = { x: event.clientX, y: event.clientY };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function handlePointerUp(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.pointerType !== "mouse") return;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    finishSwipe(event.clientX, event.clientY);
  }

  function handleTouchStart(event: ReactTouchEvent<HTMLDivElement>) {
    const touch = event.changedTouches[0];
    if (!touch) return;
    swipeStart.current = { x: touch.clientX, y: touch.clientY };
  }

  function handleTouchEnd(event: ReactTouchEvent<HTMLDivElement>) {
    const touch = event.changedTouches[0];
    if (!touch) return;
    finishSwipe(touch.clientX, touch.clientY);
  }

  function handleKeyDown(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      goBy(-1);
    }
    if (event.key === "ArrowRight") {
      event.preventDefault();
      goBy(1);
    }
  }

  return (
    <div className="drop-stack">
      <PipelineTrace
        prompt={query}
        scope={drop.source === "live-osrs-wiki" ? "deterministic breakout route" : "fallback route"}
        command={activeStrategy?.command || "osrs-ge breakouts --limit 8 --min-turnover 40m --json"}
        source={drop.source === "live-osrs-wiki" ? "OSRS Wiki latest + 24h timeseries" : "fallback snapshot"}
        generatedAt={drop.generatedAt}
      />

      <div className="drop-hero">
        <div>
          <p className="eyebrow">CLI evidence drop</p>
          <h2>{activeStrategy?.title || drop.headline}</h2>
          <p>{activeStrategy?.thesis || leader?.thesis}</p>
        </div>
        <div className="drop-clock">
          <Clock size={18} aria-hidden="true" />
          <span>{freshness}</span>
        </div>
      </div>

      <div className="drop-metrics" aria-label="Strategy drop metrics">
        <Metric icon={<Flame size={18} aria-hidden="true" />} label="Score inputs" value={activeStrategy?.scoreInputs || "trend+liq+scale"} />
        <Metric icon={<Trophy size={18} aria-hidden="true" />} label="Source" value={drop.source === "live-osrs-wiki" ? "Live wiki" : "Fallback"} />
        <Metric icon={<Activity size={18} aria-hidden="true" />} label="Streak" value={drop.streak} />
      </div>

      <div className="strategy-deck">
        <div className="deck-header">
          <div>
            <p className="eyebrow">Swipe strategy deck</p>
            <h3>{activeStrategy?.item || "Strategy"}</h3>
          </div>
          <div className="deck-controls" aria-label="Strategy deck controls">
            <button type="button" onClick={() => goToStrategy(activeIndex - 1)} aria-label="Previous strategy">
              <ChevronLeft size={16} aria-hidden="true" />
            </button>
            <span>{strategyCount ? activeIndex + 1 : 0}/{strategyCount}</span>
            <button type="button" onClick={() => goToStrategy(activeIndex + 1)} aria-label="Next strategy">
              <ChevronRight size={16} aria-hidden="true" />
            </button>
          </div>
        </div>

        <div
          className="strategy-carousel"
          role="region"
          aria-label="Swipe through active strategies"
          tabIndex={0}
          onKeyDown={handleKeyDown}
          onPointerDown={handlePointerDown}
          onPointerUp={handlePointerUp}
          onPointerCancel={() => {
            swipeStart.current = null;
          }}
          onTouchStart={handleTouchStart}
          onTouchEnd={handleTouchEnd}
        >
          <div className="strategy-track" style={{ transform: `translateX(-${activeIndex * 100}%)` }}>
            {drop.strategies.map((strategy, index) => (
              <div className="strategy-slide" aria-hidden={index !== activeIndex} key={strategy.id}>
                <StrategyCard strategy={strategy} loading={loading} onRun={onRun} capitalValue={capitalValue} />
              </div>
            ))}
          </div>
        </div>

        <div className="strategy-dots" aria-label="Strategy selector">
          {drop.strategies.map((strategy, index) => (
            <button
              type="button"
              className={index === activeIndex ? "active" : ""}
              key={strategy.id}
              onClick={() => goToStrategy(index)}
              aria-label={`Show ${strategy.item}`}
              aria-current={index === activeIndex ? "true" : undefined}
            >
              <span />
              {strategy.item}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

function StrategyCard({
  strategy,
  loading,
  onRun,
  capitalValue,
}: {
  strategy: ActiveStrategy;
  loading: boolean;
  onRun: (query: string) => Promise<void>;
  capitalValue: number;
}) {
  const [copied, setCopied] = useState(false);
  const belowScale = Number.isFinite(strategy.capitalRequiredValue) && capitalValue > 0 && strategy.capitalRequiredValue > capitalValue;

  async function copyCommand() {
    await navigator.clipboard?.writeText(strategy.command);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  }

  return (
    <article className={`strategy-card ${strategy.status}`}>
      <div className="receipt-row">
        <CommandReceipt command={strategy.command} />
        <button type="button" className="copy-command" onClick={copyCommand} title="Copy CLI command" aria-label="Copy CLI command">
          {copied ? <Check size={15} aria-hidden="true" /> : <Copy size={15} aria-hidden="true" />}
        </button>
      </div>

      <div className="strategy-heading">
        <div>
          <span className="status-chip">{strategy.status}</span>
          <h3>{strategy.item}</h3>
          <p>{strategy.setup}</p>
        </div>
        <div className="heat-score" aria-label={`${strategy.item} derived heat score`}>
          <span>Heat {strategy.heat}</span>
          <strong>{strategy.edgeScore}</strong>
          <small>{strategy.scoreInputs}</small>
        </div>
      </div>

      <div className="price-ladder" aria-label={`${strategy.item} limit bands`}>
        <div className="ladder-step entry">
          <span>Limit buy</span>
          <strong>{strategy.limitBuy}</strong>
        </div>
        <div className="ladder-step exit">
          <span>First exit</span>
          <strong>{strategy.limitSell}</strong>
        </div>
        <div className="ladder-step stretch">
          <span>Stretch</span>
          <strong>{strategy.stretchSell}</strong>
        </div>
        <div className="ladder-step stop">
          <span>Invalidation</span>
          <strong>{strategy.activeInvalidation}</strong>
        </div>
      </div>

      <dl className="evidence-line" aria-label={`${strategy.item} setup evidence`}>
        <div>
          <dt>30d</dt>
          <dd>{strategy.trend30d}</dd>
        </div>
        <div>
          <dt>90d</dt>
          <dd>{strategy.trend90d}</dd>
        </div>
        <div>
          <dt>Turnover</dt>
          <dd>{strategy.turnover30d}</dd>
        </div>
        <div>
          <dt>90d gap</dt>
          <dd>{strategy.distanceFromHigh}</dd>
        </div>
      </dl>

      <div className="live-context" aria-label={`${strategy.item} separated price context`}>
        <span>Brief P&amp;L {strategy.briefPnl}</span>
        <span>Brief ROI {strategy.briefROI}</span>
        <span>Live spread {strategy.liveSpread}</span>
        <span>Break-even {strategy.breakEvenSell}</span>
        <span>{strategy.dataFreshness}</span>
      </div>

      <div className="scale-context" aria-label={`${strategy.item} tax and scale context`}>
        <span>Tax drag {strategy.taxDrag}</span>
        <span>{strategy.briefLimitProfit}</span>
        <span>{strategy.gpPer4h}</span>
        <span>{strategy.gpPerDayMax} theoretical</span>
        <span>{strategy.capitalRequired}</span>
        <span className={belowScale ? "scale-flag below" : "scale-flag"}>{belowScale ? "below scale" : "reachable"}</span>
      </div>

      <small className="catalyst-line">{strategy.catalyst}</small>
      <small className="catalyst-line">{strategy.invalidationReason}</small>

      <details className="math-disclosure">
        <summary>Show the math</summary>
        <dl className="math-grid">
          {strategy.mathLines.map((line) => (
            <div key={`${strategy.id}-${line.label}`}>
              <dt>{line.label}</dt>
              <dd>{line.value}</dd>
            </div>
          ))}
          <div>
            <dt>gp/day(max)</dt>
            <dd>{strategy.gpPerDayMax}; theoretical ceiling assumes six full buy-limit windows, perfect presence, and complete fills.</dd>
          </div>
        </dl>
      </details>

      <div className="strategy-footer">
        <div className="tag-row">
          {strategy.tags.map((tag) => (
            <span key={tag}>{tag}</span>
          ))}
          <span>{strategy.buyLimit}</span>
        </div>
        <button className="brief-setup" type="button" disabled={loading} onClick={() => onRun(strategy.prompt)} title={strategy.prompt}>
          {loading ? <Loader2 className="spin" size={16} aria-hidden="true" /> : <Zap size={16} aria-hidden="true" />}
          Brief setup
        </button>
      </div>
    </article>
  );
}

function CommandReceipt({ command }: { command: string }) {
  return (
    <code className="command-code">
      <span className="prompt-token">$</span>
      {command.split(" ").map((part, index) => (
        <span className="command-token" key={`${part}-${index}`}>
          {part}
        </span>
      ))}
    </code>
  );
}

function PipelineTrace({
  prompt,
  scope,
  command,
  source,
  generatedAt,
}: {
  prompt: string;
  scope: string;
  command: string;
  source: string;
  generatedAt: string;
}) {
  const generated = new Date(generatedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  const nodes = [
    { label: "prompt", value: prompt },
    { label: "scope", value: scope },
    { label: "cli", value: `$ ${command}` },
    { label: "data", value: source },
    { label: "brief", value: generated },
  ];

  return (
    <div className="pipeline-trace" aria-label="Prompt to CLI evidence pipeline">
      {nodes.map((node) => (
        <div className="pipeline-node" key={node.label}>
          <span>{node.label}</span>
          <strong>{node.value}</strong>
        </div>
      ))}
    </div>
  );
}

function Metric({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
  return (
    <div className="drop-metric">
      {icon}
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function BriefView({ brief, query }: { brief: ThesisBrief; query: string }) {
  const primaryCommand = brief.nextCliCommands[0] || brief.candidates[0]?.followUpCommand || "osrs-ge agent run";
  return (
    <div className="brief-stack">
      <PipelineTrace
        prompt={query}
        scope={brief.mode === "deepseek" ? brief.model || "DeepSeek" : "deterministic preview"}
        command={primaryCommand}
        source="validated CLI evidence rows"
        generatedAt={brief.generatedAt}
      />

      <div className="brief-head">
        <div>
          <p className="eyebrow">{brief.mode === "deepseek" ? brief.model : "preview mode"}</p>
          <h2>{brief.queryIntent}</h2>
        </div>
        <span>{new Date(brief.generatedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>
      </div>

      <p className="thesis">{brief.thesis}</p>

      <div className="candidate-grid">
        {brief.candidates.map((candidate) => (
          <article className="candidate" key={`${candidate.item}-${candidate.sourceProbe}`}>
            <div className="candidate-top">
              <strong>{candidate.item}</strong>
              <span>{candidate.confidence}</span>
            </div>
            <p>{candidate.setup}</p>
            <small>{candidate.evidence}</small>
            <div className="risk">{candidate.risk}</div>
            <code>{candidate.followUpCommand}</code>
          </article>
        ))}
      </div>

      <div className="command-bar">
        {brief.nextCliCommands.map((command) => (
          <code key={command}>{command}</code>
        ))}
      </div>

      <div className="trap-line">
        {brief.rejectedTraps.slice(0, 3).map((trap) => (
          <span key={trap}>{trap}</span>
        ))}
      </div>
    </div>
  );
}

function formatTimeLeft(nextRefreshAt: string, now: number) {
  const ms = Math.max(0, new Date(nextRefreshAt).getTime() - now);
  const hours = Math.floor(ms / 3_600_000);
  const minutes = Math.floor((ms % 3_600_000) / 60_000);
  if (hours <= 0 && minutes <= 0) return "refreshing";
  if (hours <= 0) return `${minutes}m left`;
  return `${hours}h ${minutes}m left`;
}

function parseCapital(value: string) {
  const normalized = value.trim().toLowerCase().replaceAll(",", "").replaceAll("_", "");
  const match = normalized.match(/^(\d+(?:\.\d+)?)([kmb])?$/);
  if (!match) return 0;
  const amount = Number.parseFloat(match[1]);
  const suffix = match[2];
  if (!Number.isFinite(amount)) return 0;
  if (suffix === "b") return amount * 1_000_000_000;
  if (suffix === "m") return amount * 1_000_000;
  if (suffix === "k") return amount * 1_000;
  return amount;
}
