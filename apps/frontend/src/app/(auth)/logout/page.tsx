"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function LogoutPage() {
  const router = useRouter();

  useEffect(() => {
    fetch("/api/auth/logout", { method: "POST" }).finally(() => {
      router.push("/login");
    });
  }, [router]);

  return (
    <div className="nm-card text-center">
      <p className="text-[var(--color-text-muted)]">Signing out...</p>
    </div>
  );
}
