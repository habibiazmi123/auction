"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { Bell } from "lucide-react";

export function Navbar() {
  const pathname = usePathname();
  const router = useRouter();

  async function handleLogout() {
    await fetch("/api/auth/logout", { method: "POST" });
    router.push("/login");
  }

  return (
    <nav className="nm-surface fixed top-4 left-4 right-4 z-50 flex items-center justify-between px-6 py-3">
      <Link href="/auctions" className="text-xl font-bold text-[var(--color-primary)]">
        Auction
      </Link>

      <div className="flex items-center gap-4">
        <Link
          href="/notifications"
          className={`p-2 rounded-xl ${
            pathname === "/notifications" ? "nm-inset" : "nm-btn"
          }`}
        >
          <Bell className="w-5 h-5" />
        </Link>
        <button onClick={handleLogout} className="nm-btn text-sm">
          Sign Out
        </button>
      </div>
    </nav>
  );
}
