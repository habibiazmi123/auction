"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Gavel, Package, Receipt } from "lucide-react";

const links = [
  { href: "/auctions", label: "Auctions", icon: Gavel },
  { href: "/products", label: "Products", icon: Package },
  { href: "/settlements", label: "Settlements", icon: Receipt },
];

export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="nm-surface fixed left-4 top-24 bottom-4 w-56 p-4 flex flex-col gap-2">
      {links.map(({ href, label, icon: Icon }) => {
        const active = pathname.startsWith(href);
        return (
          <Link
            key={href}
            href={href}
            className={`flex items-center gap-3 px-4 py-3 rounded-xl text-sm font-medium transition-all
              ${active ? "nm-inset text-[var(--color-primary)]" : "nm-btn"}`}
          >
            <Icon className="w-4 h-4" />
            {label}
          </Link>
        );
      })}
    </aside>
  );
}
