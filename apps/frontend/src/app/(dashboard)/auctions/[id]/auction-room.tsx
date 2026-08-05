"use client";

import { useEffect, useState } from "react";
import { AuctionTimer } from "@/components/auction/auction-timer";
import { LiveIndicator } from "@/components/auction/live-indicator";
import { BidPanel } from "@/components/auction/bid-panel";
import { BidHistory } from "@/components/auction/bid-history";
import { wsManager } from "@/lib/ws";

interface AuctionRoomProps {
  auctionId: string;
  endsAt: string;
  currentPriceCents: number;
  minimumIncrementCents: number;
  isLive: boolean;
  isClosed: boolean;
  winnerId: string | null;
}

export function AuctionRoom({
  auctionId,
  endsAt,
  currentPriceCents,
  minimumIncrementCents,
  isLive,
  isClosed,
  winnerId,
}: AuctionRoomProps) {
  const [connected, setConnected] = useState(false);
  const [token, setToken] = useState<string | null>(null);

  useEffect(() => {
    const match = document.cookie.match(/access_token=([^;]+)/);
    setToken(match?.[1] || null);
  }, []);

  useEffect(() => {
    if (!token || !isLive) return;

    wsManager.connect(auctionId, token);

    const unsubConnected = wsManager.on("connected", () => setConnected(true));
    const unsubDisconnected = wsManager.on("disconnected", () => setConnected(false));

    return () => {
      unsubConnected();
      unsubDisconnected();
      wsManager.disconnect();
    };
  }, [auctionId, token, isLive]);

  const displayPrice = `$${(currentPriceCents / 100).toFixed(2)}`;

  return (
    <div className="space-y-6">
      <div className="nm-card flex items-center justify-between">
        <div>
          <p className="text-xs text-[var(--color-text-muted)] uppercase tracking-wider">Current Price</p>
          <p className="text-4xl font-bold text-[var(--color-primary)]">{displayPrice}</p>
        </div>
        <LiveIndicator connected={connected} />
      </div>

      <AuctionTimer endsAt={endsAt} />

      {isClosed && (
        <div className="nm-card text-center">
          <p className="text-lg font-semibold text-[var(--color-accent)]">Auction Closed</p>
          {winnerId && (
            <p className="text-sm text-[var(--color-text-muted)] mt-1">
              Winner: {winnerId.slice(0, 8)}...
            </p>
          )}
        </div>
      )}

      {isLive && (
        <>
          <div className="nm-card">
            <h2 className="font-semibold mb-3">Place Your Bid</h2>
            <BidPanel
              auctionId={auctionId}
              minimumIncrementCents={minimumIncrementCents}
              currentPriceCents={currentPriceCents}
              disabled={!connected}
            />
          </div>

          <div className="nm-card">
            <h2 className="font-semibold mb-3">Bid History</h2>
            <BidHistory auctionId={auctionId} />
          </div>
        </>
      )}
    </div>
  );
}
