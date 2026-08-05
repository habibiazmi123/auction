export interface User {
  id: string;
  email: string;
  role: "buyer" | "seller" | "admin";
  created_at: string;
  updated_at: string;
}

export interface AuthTokens {
  access_token: string;
  refresh_token: string;
}

export interface Product {
  id: string;
  seller_id: string;
  name: string;
  description: string;
  quantity: number;
  status: "draft" | "available" | "locked";
  auction_id: string | null;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface Auction {
  id: string;
  product_id: string;
  seller_id: string;
  product_name: string;
  product_description: string;
  product_quantity: number;
  status: "draft" | "scheduled" | "live" | "closed";
  starting_price_cents: number;
  current_price_cents: number;
  current_winner_id: string | null;
  minimum_increment_cents: number;
  starts_at: string;
  ends_at: string;
  anti_sniping_window_seconds: number;
  anti_sniping_extension_seconds: number;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface Bid {
  id: string;
  auction_id: string;
  bidder_id: string;
  command_id: string;
  idempotency_key: string;
  amount_cents: number;
  status: "accepted" | "rejected";
  rejection_code: string | null;
  created_at: string;
}

export interface Settlement {
  id: string;
  auction_id: string;
  seller_id: string;
  buyer_id: string;
  bid_id: string;
  amount_cents: number;
  status: "pending" | "paid" | "failed";
  source_event_id: string;
  created_at: string;
  updated_at: string;
}

export interface Notification {
  id: string;
  source_event_id: string;
  recipient_id: string;
  type: string;
  payload: Record<string, unknown>;
  read_at: string | null;
  created_at: string;
}

export interface BidPlacedEvent {
  bid_id: string;
  auction_id: string;
  bidder_id: string;
  amount_cents: number;
  status: string;
  rejection_code: string | null;
}

export interface AuctionClosedEvent {
  auction_id: string;
  seller_id: string;
  bid_id: string;
  final_price_cents: number;
  winner_id: string;
}

export interface PaginatedResponse<T> {
  items: T[];
  page: number;
  page_size: number;
  total: number;
}

export interface ApiError {
  error: string;
}

export interface BidCommand {
  auction_id: string;
  amount_cents: number;
  idempotency_key: string;
}

export interface CreateProductRequest {
  name: string;
  description: string;
  quantity: number;
}

export interface CreateAuctionRequest {
  product_id: string;
  starting_price_cents: number;
  minimum_increment_cents: number;
  starts_at: string;
  ends_at: string;
  anti_sniping_window_seconds: number;
  anti_sniping_extension_seconds: number;
}
