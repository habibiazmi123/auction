"use client";

import { Suspense, useState, FormEvent } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

function NewAuctionForm() {
  const router = useRouter();
  const params = useSearchParams();
  const productId = params.get("product_id") || "";
  const productName = params.get("name") || "";

  const [startingPrice, setStartingPrice] = useState("");
  const [minIncrement, setMinIncrement] = useState("1");
  const [startsAt, setStartsAt] = useState("");
  const [endsAt, setEndsAt] = useState("");
  const [antiSnipingWindow, setAntiSnipingWindow] = useState("30");
  const [antiSnipingExtension, setAntiSnipingExtension] = useState("60");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    try {
      const res = await fetch("/v1/auctions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          product_id: productId,
          starting_price_cents: Math.round(parseFloat(startingPrice) * 100),
          minimum_increment_cents: Math.round(parseFloat(minIncrement) * 100),
          starts_at: new Date(startsAt).toISOString(),
          ends_at: new Date(endsAt).toISOString(),
          anti_sniping_window_seconds: parseInt(antiSnipingWindow, 10),
          anti_sniping_extension_seconds: parseInt(antiSnipingExtension, 10),
        }),
      });

      if (!res.ok) {
        const data = await res.json();
        setError(data.error || "Failed to create auction");
        return;
      }

      const auction = await res.json();
      router.push(`/auctions/${auction.id}`);
    } catch {
      setError("Network error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="max-w-lg mx-auto space-y-6">
      <h1 className="text-2xl font-bold">Create Auction</h1>
      {productName && (
        <p className="text-[var(--color-text-muted)]">
          Product: <span className="font-medium text-[var(--color-text)]">{productName}</span>
        </p>
      )}

      <form onSubmit={handleSubmit} className="nm-card space-y-5">
        <div className="space-y-2">
          <Label htmlFor="sp">Starting Price ($)</Label>
          <Input id="sp" type="number" step="0.01" min="0.01" required value={startingPrice} onChange={(e) => setStartingPrice(e.target.value)} className="nm-input w-full" />
        </div>

        <div className="space-y-2">
          <Label htmlFor="mi">Minimum Increment ($)</Label>
          <Input id="mi" type="number" step="0.01" min="0.01" required value={minIncrement} onChange={(e) => setMinIncrement(e.target.value)} className="nm-input w-full" />
        </div>

        <div className="space-y-2">
          <Label htmlFor="sa">Starts At</Label>
          <Input id="sa" type="datetime-local" required value={startsAt} onChange={(e) => setStartsAt(e.target.value)} className="nm-input w-full" />
        </div>

        <div className="space-y-2">
          <Label htmlFor="ea">Ends At</Label>
          <Input id="ea" type="datetime-local" required value={endsAt} onChange={(e) => setEndsAt(e.target.value)} className="nm-input w-full" />
        </div>

        <details className="text-sm">
          <summary className="text-[var(--color-text-muted)] cursor-pointer">Anti-Sniping Settings</summary>
          <div className="space-y-4 mt-3 pt-3 border-t border-[var(--color-border)]">
            <div className="space-y-2">
              <Label htmlFor="asw">Window (seconds)</Label>
              <Input id="asw" type="number" min="0" value={antiSnipingWindow} onChange={(e) => setAntiSnipingWindow(e.target.value)} className="nm-input w-full" />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ase">Extension (seconds)</Label>
              <Input id="ase" type="number" min="0" value={antiSnipingExtension} onChange={(e) => setAntiSnipingExtension(e.target.value)} className="nm-input w-full" />
            </div>
          </div>
        </details>

        {error && (
          <div className="nm-inset p-3 text-sm text-[var(--color-accent)] border-l-4 border-[var(--color-accent)]">
            {error}
          </div>
        )}

        <Button type="submit" disabled={loading || !productId} className="nm-btn nm-btn-primary w-full">
          {loading ? "Creating..." : "Create Auction"}
        </Button>
      </form>
    </div>
  );
}

export default function NewAuctionPage() {
  return (
    <Suspense>
      <NewAuctionForm />
    </Suspense>
  );
}
