import { NextRequest, NextResponse } from "next/server";
import { setAuthCookies } from "@/lib/auth";

export async function POST(request: NextRequest) {
  const body = await request.json();

  const res = await fetch(`${process.env.API_GATEWAY_URL || "http://localhost:8080"}/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

  if (!res.ok) {
    const data = await res.json().catch(() => ({ error: "Login failed" }));
    return NextResponse.json(data, { status: res.status });
  }

  const { access_token, refresh_token } = await res.json();
  await setAuthCookies(access_token, refresh_token);

  return NextResponse.json({ ok: true });
}
