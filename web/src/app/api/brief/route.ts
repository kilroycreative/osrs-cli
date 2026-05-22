import { NextResponse } from "next/server";
import { buildThesisBrief } from "@/lib/brief";

export const runtime = "nodejs";

export async function POST(request: Request) {
  try {
    const body = (await request.json()) as { query?: unknown };
    const query = typeof body.query === "string" ? body.query : "";
    const brief = await buildThesisBrief(query);
    return NextResponse.json(brief);
  } catch (error) {
    const message = error instanceof Error ? error.message : "Unable to build thesis.";
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
