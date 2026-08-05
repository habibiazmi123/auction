"use client";

import { useEffect, useState } from "react";
import { wsManager } from "@/lib/ws";
import type { BidPlacedEvent } from "@/lib/types";

interface BidEntry {
  id: string;
  bidder: string;
  amount: number;
  status: string;
  timestamp: string;
}

export function BidHistory({ auctionId }: { auctionId: string }) {
  const [bids, setBids] = useState<BidEntry[]>([]);

  useEffect(() => {
    const unsub = wsManager.on("BidPlaced", (data) => {
      const event = data as BidPlacedEvent & { occurred_at?: string };
      if (!event.bid_id) return;

      setBids((prev) => [
        {
          id: event.bid_id,
          bidder: event.bidder_id.slice(0, 8) + "...",
          amount: event.amount_cents,
          status: event.status,
          timestamp: event.occurred_at || new Date().toISOString(),
        },
        ...prev,
      ]);
    });

    return () => unsub();
  }, [auctionId]);

  if (bids.length === 0) {
    return (
      <div className="text-center py-8 text-[var(--color-text-muted)]">
        No bids yet. Be the first!
      </div>
    );
  }

  return (
    <div className="space-y-2 max-h-80 overflow-y-auto">
      {bids.map((bid) => (
        <div key={bid.id} className="nm-inset p-3 flex items-center justify-between">
          <div>
            <p className="font-medium text-sm">
              ${(bid.amount / 100).toFixed(2)}
            </p>
            <p className="text-xs text-[var(--color-text-muted)]">{bid.bidder}</p>
          </div>
          <span
            className={`text-xs px-2 py-1 rounded-full ${
              bid.status === "accepted"
                ? "bg-green-100 text-green-700"
                : "bg-red-100 text-red-700"
            }`}
          >
            {bid.status}
          </span>
        </div>
      ))}
    </div>
  );
}
