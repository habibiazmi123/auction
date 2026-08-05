import { NextRequest, NextResponse } from "next/server";

export async function middleware(request: NextRequest) {
  const accessToken = request.cookies.get("access_token")?.value;
  const refreshToken = request.cookies.get("refresh_token")?.value;

  if (accessToken) return NextResponse.next();

  if (!refreshToken) {
    return NextResponse.redirect(new URL("/login", request.url));
  }

  try {
    const res = await fetch(`${process.env.API_GATEWAY_URL || "http://localhost:8080"}/v1/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refreshToken }),
    });

    if (!res.ok) {
      return clearAndRedirect(request);
    }

    const data = await res.json();
    const response = NextResponse.next();

    response.cookies.set("access_token", data.access_token, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      sameSite: "strict",
      path: "/",
      maxAge: 15 * 60,
    });
    response.cookies.set("refresh_token", data.refresh_token, {
      httpOnly: true,
      secure: process.env.NODE_ENV === "production",
      sameSite: "strict",
      path: "/api/auth/refresh",
      maxAge: 7 * 24 * 60 * 60,
    });

    return response;
  } catch {
    return clearAndRedirect(request);
  }
}

function clearAndRedirect(request: NextRequest) {
  const response = NextResponse.redirect(new URL("/login", request.url));
  response.cookies.delete("access_token");
  response.cookies.delete("refresh_token");
  return response;
}

export const config = {
  matcher: ["/(dashboard)/:path*", "/auctions/:path*", "/products/:path*", "/settlements", "/notifications"],
};
