import { NextRequest, NextResponse } from "next/server";
import { clearAuthCookies, getRefreshToken } from "@/lib/auth";

export async function POST(_request: NextRequest) {
  const refreshToken = await getRefreshToken();
  if (refreshToken) {
    await fetch(`${process.env.API_GATEWAY_URL || "http://localhost:8080"}/v1/auth/logout`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refreshToken }),
    }).catch(() => {});
  }

  await clearAuthCookies();
  return NextResponse.json({ ok: true });
}
