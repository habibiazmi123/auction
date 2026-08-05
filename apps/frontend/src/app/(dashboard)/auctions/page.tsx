import { api } from "@/lib/api"
import { AuctionsGrid } from "./auctions-grid"

export default async function AuctionsPage() {
  let data;
  try {
    data = await api.getAuctions();
  } catch {
    return (
      <div className="nm-card text-center">
        <p className="text-[var(--color-text-muted)] mb-4">Failed to load auctions. Is the API running?</p>
        <a href="/auctions" className="nm-btn nm-btn-primary inline-block">
          Retry
        </a>
      </div>
    );
  }

  const items = data?.items || [];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Auctions</h1>
        <p className="text-[var(--color-text-muted)]">Browse live and upcoming auctions</p>
      </div>

      <div className="nm-card overflow-hidden">
        <AuctionsGrid data={items} />
      </div>
    </div>
  );
}
