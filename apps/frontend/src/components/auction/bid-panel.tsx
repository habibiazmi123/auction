"use client";

import { useState, FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";

interface BidPanelProps {
  auctionId: string;
  minimumIncrementCents: number;
  currentPriceCents: number;
  disabled: boolean;
}

export function BidPanel({ auctionId, minimumIncrementCents, currentPriceCents, disabled }: BidPanelProps) {
  const minBidCents = currentPriceCents + minimumIncrementCents;
  const minBidDollars = (minBidCents / 100).toFixed(2);
  const [amount, setAmount] = useState(minBidDollars);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const parsed = parseFloat(amount);
    if (isNaN(parsed)) {
      setError("Enter a valid amount");
      setLoading(false);
      return;
    }

    const amountCents = Math.round(parsed * 100);
    if (amountCents < minBidCents) {
      setError(`Minimum bid is $${minBidDollars}`);
      setLoading(false);
      return;
    }

    try {
      const res = await fetch(`/v1/auctions/${auctionId}/bids`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          auction_id: auctionId,
          amount_cents: amountCents,
          idempotency_key: crypto.randomUUID(),
        }),
      });

      if (!res.ok) {
        const data = await res.json();
        setError(data.error || "Bid rejected");
        return;
      }

      toast.success("Bid placed!");
    } catch {
      setError("Network error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3">
      {error && (
        <div className="nm-inset p-3 text-sm text-[var(--color-accent)] border-l-4 border-[var(--color-accent)]">
          {error}
        </div>
      )}

      <div className="flex gap-3">
        <Input
          type="number"
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          disabled={disabled || loading}
          className="nm-input flex-1"
          placeholder={`Min: $${minBidDollars}`}
        />
        <Button
          type="submit"
          disabled={disabled || loading}
          className="nm-btn nm-btn-primary"
        >
          {loading ? "Bidding..." : "Place Bid"}
        </Button>
      </div>
    </form>
  );
}
