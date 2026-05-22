import { QueryWorkbench } from "@/components/query-workbench";
import { buildStrategyDrop } from "@/lib/strategies";

export const dynamic = "force-dynamic";

export default async function Home() {
  const strategyDrop = await buildStrategyDrop();

  return (
    <main className="app-shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">Printing Press / OSRS GE</p>
          <h1>GE Thesis</h1>
        </div>
        <nav className="nav-pills" aria-label="Product status">
          <span>{strategyDrop.cycleLabel}</span>
          <span>{strategyDrop.streak}</span>
          <span>DeepSeek briefs</span>
        </nav>
      </header>

      <section className="workspace" aria-label="GE Thesis workbench">
        <QueryWorkbench initialDrop={strategyDrop} />
      </section>
    </main>
  );
}
