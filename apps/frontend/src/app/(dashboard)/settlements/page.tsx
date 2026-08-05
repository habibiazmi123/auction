import { Badge } from "@/components/ui/badge";
import type { Settlement } from "@/lib/types";
import { cookies } from "next/headers";

function formatCents(cents: number) {
  return `$${(cents / 100).toFixed(2)}`;
}

export default async function SettlementsPage() {
  let items: Settlement[] = [];

  try {
    const token = (await cookies()).get("access_token")?.value;
    const res = await fetch(
      `${process.env.API_GATEWAY_URL || "http://localhost:8080"}/v1/auctions/dummy/settlement`,
      { headers: token ? { Authorization: `Bearer ${token}` } : {} },
    );
    if (res.ok) {
      items = [await res.json()];
    }
  } catch {}

  const statusColors: Record<string, string> = {
    paid: "bg-green-100 text-green-700",
    pending: "bg-amber-100 text-amber-700",
    failed: "bg-red-100 text-red-700",
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Settlements</h1>
        <p className="text-[var(--color-text-muted)]">Your transaction history</p>
      </div>

      {items.length > 0 ? (
        <div className="nm-card overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--color-border)] text-left text-[var(--color-text-muted)]">
                <th className="px-4 py-3 font-medium">Auction</th>
                <th className="px-4 py-3 font-medium">Amount</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium">Date</th>
              </tr>
            </thead>
            <tbody>
              {items.map((s) => (
                <tr key={s.id} className="border-b border-[var(--color-border)] last:border-0">
                  <td className="px-4 py-3 font-mono text-xs">{s.auction_id.slice(0, 8)}...</td>
                  <td className="px-4 py-3">{formatCents(s.amount_cents)}</td>
                  <td className="px-4 py-3">
                    <Badge className={statusColors[s.status] || ""}>{s.status}</Badge>
                  </td>
                  <td className="px-4 py-3 text-[var(--color-text-muted)]">
                    {new Date(s.created_at).toLocaleDateString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="nm-card text-center py-12">
          <p className="text-[var(--color-text-muted)]">No settlements yet.</p>
        </div>
      )}
    </div>
  );
}
