import Link from "next/link";
import { api } from "@/lib/api";
import { ProductsGrid } from "./products-grid";

export default async function ProductsPage() {
  let data;
  try {
    data = await api.getProducts();
  } catch {
    return (
      <div className="nm-card text-center">
        <p className="text-[var(--color-text-muted)] mb-4">Failed to load products.</p>
        <a href="/products" className="nm-btn nm-btn-primary inline-block">Retry</a>
      </div>
    );
  }

  const items = data?.items || [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Products</h1>
          <p className="text-[var(--color-text-muted)]">Manage your product catalog</p>
        </div>
        <Link href="/products/new" className="nm-btn nm-btn-primary">
          New Product
        </Link>
      </div>

      <div className="nm-card overflow-hidden">
        <ProductsGrid data={items} />
      </div>
    </div>
  );
}
