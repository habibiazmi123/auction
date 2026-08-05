"use client";

import { useState, FormEvent } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export default function NewProductPage() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [quantity, setQuantity] = useState("1");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    try {
      const res = await fetch("/v1/products", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name,
          description,
          quantity: parseInt(quantity, 10),
        }),
      });

      if (!res.ok) {
        const data = await res.json();
        setError(data.error || "Failed to create product");
        return;
      }

      router.push("/products");
    } catch {
      setError("Network error");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="max-w-lg mx-auto space-y-6">
      <h1 className="text-2xl font-bold">New Product</h1>

      <form onSubmit={handleSubmit} className="nm-card space-y-5">
        <div className="space-y-2">
          <Label htmlFor="name">Name</Label>
          <Input id="name" required value={name} onChange={(e) => setName(e.target.value)} className="nm-input w-full" />
        </div>

        <div className="space-y-2">
          <Label htmlFor="desc">Description</Label>
          <textarea
            id="desc"
            required
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="nm-input w-full min-h-[100px] resize-y"
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="qty">Quantity</Label>
          <Input id="qty" type="number" min="1" required value={quantity} onChange={(e) => setQuantity(e.target.value)} className="nm-input w-full" />
        </div>

        {error && (
          <div className="nm-inset p-3 text-sm text-[var(--color-accent)] border-l-4 border-[var(--color-accent)]">
            {error}
          </div>
        )}

        <div className="flex gap-3">
          <Button type="button" onClick={() => router.back()} className="nm-btn flex-1">
            Cancel
          </Button>
          <Button type="submit" disabled={loading} className="nm-btn nm-btn-primary flex-1">
            {loading ? "Creating..." : "Create Product"}
          </Button>
        </div>
      </form>
    </div>
  );
}
