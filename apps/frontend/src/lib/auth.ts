import { cookies } from "next/headers";

export async function setAuthCookies(accessToken: string, refreshToken: string) {
  const store = await cookies();
  store.set("access_token", accessToken, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "strict",
    path: "/",
    maxAge: 15 * 60,
  });
  store.set("refresh_token", refreshToken, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "strict",
    path: "/api/auth/refresh",
    maxAge: 7 * 24 * 60 * 60,
  });
}

export async function clearAuthCookies() {
  const store = await cookies();
  store.delete("access_token");
  store.delete("refresh_token");
}

export async function getAccessToken(): Promise<string | undefined> {
  return (await cookies()).get("access_token")?.value;
}

export async function getRefreshToken(): Promise<string | undefined> {
  return (await cookies()).get("refresh_token")?.value;
}
