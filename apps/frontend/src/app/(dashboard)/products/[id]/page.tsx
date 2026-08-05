import Link from "next/link";
import { api } from "@/lib/api";

export default async function ProductDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  let product;
  try {
    product = await api.getProduct(id);
  } catch {
    return (
      <div className="nm-card text-center">
        <p className="text-[var(--color-text-muted)] mb-4">Product not found.</p>
        <a href="/products" className="nm-btn nm-btn-primary inline-block">Back to Products</a>
      </div>
    );
  }

  return (
    <div className="max-w-lg mx-auto space-y-6">
      <h1 className="text-2xl font-bold">{product.name}</h1>

      <div className="nm-card space-y-4">
        <div>
          <p className="text-xs text-[var(--color-text-muted)] uppercase">Description</p>
          <p className="text-sm">{product.description}</p>
        </div>
        <div className="flex gap-8">
          <div>
            <p className="text-xs text-[var(--color-text-muted)] uppercase">Quantity</p>
            <p className="font-semibold">{product.quantity}</p>
          </div>
          <div>
            <p className="text-xs text-[var(--color-text-muted)] uppercase">Status</p>
            <p className="font-semibold capitalize">{product.status}</p>
          </div>
        </div>
        <div>
          <p className="text-xs text-[var(--color-text-muted)] uppercase">Created</p>
          <p className="text-sm">{new Date(product.created_at).toLocaleString()}</p>
        </div>

        {product.status === "available" && (
          <Link
            href={`/auctions/new?product_id=${product.id}&name=${encodeURIComponent(product.name)}`}
            className="nm-btn nm-btn-primary inline-block w-full text-center"
          >
            Create Auction
          </Link>
        )}
      </div>
    </div>
  );
}
