"use client";

import { useState, useEffect } from "react";

export function AuctionTimer({ endsAt }: { endsAt: string }) {
  const [remaining, setRemaining] = useState("");

  useEffect(() => {
    function tick() {
      const diff = new Date(endsAt).getTime() - Date.now();
      if (diff <= 0) {
        setRemaining("Ended");
        return;
      }

      const h = Math.floor(diff / 3600000);
      const m = Math.floor((diff % 3600000) / 60000);
      const s = Math.floor((diff % 60000) / 1000);
      setRemaining(
        h > 0
          ? `${h}h ${String(m).padStart(2, "0")}m ${String(s).padStart(2, "0")}s`
          : `${m}m ${String(s).padStart(2, "0")}s`
      );
    }

    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [endsAt]);

  const isUrgent = new Date(endsAt).getTime() - Date.now() < 60000 && remaining !== "Ended";

  return (
    <div
      className={`text-center p-4 ${isUrgent ? "nm-pulse" : ""}`}
    >
      <p className="text-xs text-[var(--color-text-muted)] uppercase tracking-wider">Time Remaining</p>
      <p className={`text-3xl font-mono font-bold mt-1 ${isUrgent ? "text-[var(--color-accent)]" : "text-[var(--color-text)]"}`}>
        {remaining}
      </p>
    </div>
  );
}
