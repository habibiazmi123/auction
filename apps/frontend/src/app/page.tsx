import Link from "next/link";
import { redirect } from "next/navigation";
import { getAccessToken } from "@/lib/auth";

export default async function LandingPage() {
  if (await getAccessToken()) redirect("/auctions");

  return (
    <div className="min-h-screen flex flex-col items-center justify-center bg-[var(--color-bg)] gap-8 p-4">
      <div className="text-center space-y-4">
        <h1 className="text-5xl font-bold text-[var(--color-text)]">
          Auction Platform
        </h1>
        <p className="text-lg text-[var(--color-text-muted)] max-w-md">
          Real-time bidding. No compromises. Create auctions, place bids, and settle instantly.
        </p>
      </div>

      <div className="flex gap-4">
        <Link href="/register" className="nm-btn nm-btn-primary text-lg px-8 py-3">
          Get Started
        </Link>
        <Link href="/login" className="nm-btn text-lg px-8 py-3">
          Sign In
        </Link>
      </div>
    </div>
  );
}
