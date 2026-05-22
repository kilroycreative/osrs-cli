import { buildStrategyDrop } from "@/lib/strategies";

export const dynamic = "force-dynamic";

export async function GET(request: Request) {
  const cronSecret = process.env.CRON_SECRET;
  if (!cronSecret) {
    return Response.json({ ok: false, error: "CRON_SECRET is not configured." }, { status: 500 });
  }

  const authHeader = request.headers.get("authorization");
  if (authHeader !== `Bearer ${cronSecret}`) {
    return Response.json({ ok: false, error: "Unauthorized" }, { status: 401 });
  }

  const drop = await buildStrategyDrop();
  return Response.json(
    {
      ok: true,
      refreshedAt: drop.generatedAt,
      cycleId: drop.cycleId,
      cycleLabel: drop.cycleLabel,
      nextRefreshAt: drop.nextRefreshAt,
      staleAt: drop.staleAt,
      strategyCount: drop.strategies.length,
      strategies: drop.strategies.map((strategy) => ({
        id: strategy.id,
        item: strategy.item,
        status: strategy.status,
        edgeScore: strategy.edgeScore,
        heat: strategy.heat,
      })),
    },
    {
      headers: {
        "Cache-Control": "no-store, max-age=0",
      },
    },
  );
}
