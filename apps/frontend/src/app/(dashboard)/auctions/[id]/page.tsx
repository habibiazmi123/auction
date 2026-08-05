import { api } from "@/lib/api";
import { getAccessToken } from "@/lib/auth";
import { AuctionRoom } from "./auction-room";

export default async function AuctionPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  let auction;
  try {
    auction = await api.getAuction(id);
  } catch {
    return (
      <div className="nm-card text-center space-y-4">
        <p className="text-[var(--color-text-muted)]">Auction not found or API unavailable.</p>
        <a href="/auctions" className="nm-btn nm-btn-primary inline-block">
          Back to Auctions
        </a>
      </div>
    );
  }

  const isLive = auction.status === "live";
  const isClosed = auction.status === "closed";
  const accessToken = await getAccessToken();

  return (
    <div className="space-y-6">
      <div className="nm-card space-y-2">
        <h1 className="text-2xl font-bold">{auction.product_name}</h1>
        <p className="text-[var(--color-text-muted)]">{auction.product_description}</p>
        <p className="text-sm text-[var(--color-text-muted)]">
          Starting price: ${(auction.starting_price_cents / 100).toFixed(2)}
        </p>
      </div>

      <AuctionRoom
        auctionId={auction.id}
        endsAt={auction.ends_at}
        currentPriceCents={auction.current_price_cents || auction.starting_price_cents}
        minimumIncrementCents={auction.minimum_increment_cents}
        isLive={isLive}
        isClosed={isClosed}
        winnerId={auction.current_winner_id}
        accessToken={accessToken || ""}
      />
    </div>
  );
}
