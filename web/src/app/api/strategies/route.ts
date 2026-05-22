import { buildStrategyDrop } from "@/lib/strategies";

export const dynamic = "force-dynamic";

export async function GET() {
  return Response.json(await buildStrategyDrop(), {
    headers: {
      "Cache-Control": "s-maxage=300, stale-while-revalidate=120",
    },
  });
}
