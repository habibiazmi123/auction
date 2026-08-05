import { cookies } from "next/headers";
import type {
  User,
  Product,
  Auction,
  Bid,
  Settlement,
  Notification,
  PaginatedResponse,
  BidCommand,
  CreateProductRequest,
  CreateAuctionRequest,
} from "@/lib/types";

const BASE = process.env.API_GATEWAY_URL || "http://localhost:8080";

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const accessToken = (await cookies()).get("access_token")?.value;

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
    ...((init?.headers as Record<string, string>) || {}),
  };

  const res = await fetch(`${BASE}${path}`, { ...init, headers });

  if (!res.ok) {
    const body = await res.text();
    throw new Error(`API ${res.status}: ${body}`);
  }

  return res.json();
}

export const api = {
  getAuctions: () =>
    fetchJSON<PaginatedResponse<Auction>>("/v1/auctions"),

  getAuction: (id: string) =>
    fetchJSON<Auction>(`/v1/auctions/${id}`),

  placeBid: (auctionId: string, body: BidCommand) =>
    fetchJSON<{ bid_id: string }>(`/v1/auctions/${auctionId}/bids`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  getProducts: () =>
    fetchJSON<PaginatedResponse<Product>>("/v1/products"),

  getProduct: (id: string) =>
    fetchJSON<Product>(`/v1/products/${id}`),

  createProduct: (body: CreateProductRequest) =>
    fetchJSON<Product>("/v1/products", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  createAuction: (body: CreateAuctionRequest) =>
    fetchJSON<Auction>("/v1/auctions", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  getSettlement: (auctionId: string) =>
    fetchJSON<Settlement>(`/v1/auctions/${auctionId}/settlement`),

  getNotifications: () =>
    fetchJSON<Notification[]>("/v1/notifications"),

  getBid: (bidId: string) =>
    fetchJSON<Bid>(`/v1/bids/${bidId}`),
};
