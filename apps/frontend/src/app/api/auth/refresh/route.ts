import { NextRequest, NextResponse } from "next/server";
import { setAuthCookies, getRefreshToken, clearAuthCookies } from "@/lib/auth";

export async function POST(request: NextRequest) {
  const refreshToken = await getRefreshToken();
  if (!refreshToken) {
    return NextResponse.json({ error: "No refresh token" }, { status: 401 });
  }

  const res = await fetch(`${process.env.API_GATEWAY_URL || "http://localhost:8080"}/v1/auth/refresh`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refresh_token: refreshToken }),
  });

  if (!res.ok) {
    await clearAuthCookies();
    return NextResponse.json({ error: "Refresh failed" }, { status: 401 });
  }

  const { access_token, refresh_token } = await res.json();
  await setAuthCookies(access_token, refresh_token);

  return NextResponse.json({ ok: true });
}
