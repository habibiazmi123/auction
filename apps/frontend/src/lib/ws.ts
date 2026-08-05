type WsEventCallback = (data: unknown) => void;

class WsManager {
  private ws: WebSocket | null = null;
  private listeners: Map<string, Set<WsEventCallback>> = new Map();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private backoff = 1000;
  private url = "";

  connect(auctionId: string, token: string) {
    this.disconnect();
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    this.url = `${protocol}//${location.host}/v1/auctions/${auctionId}/live?access_token=${token}`;
    this.open();
  }

  private open() {
    try {
      this.ws = new WebSocket(this.url, "auction-live");
    } catch {
      this.scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      this.backoff = 1000;
      this.emit("connected", {});
    };

    this.ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        this.emit(msg.event_type || "message", msg);
      } catch {
        // ignore malformed messages
      }
    };

    this.ws.onclose = () => {
      this.emit("disconnected", {});
      this.scheduleReconnect();
    };

    this.ws.onerror = () => {
      this.ws?.close();
    };
  }

  private scheduleReconnect() {
    this.reconnectTimer = setTimeout(() => {
      this.backoff = Math.min(this.backoff * 2, 30000);
      this.open();
    }, this.backoff);
  }

  on(event: string, cb: WsEventCallback) {
    if (!this.listeners.has(event)) this.listeners.set(event, new Set());
    this.listeners.get(event)!.add(cb);
    return () => {
      this.listeners.get(event)?.delete(cb);
    };
  }

  private emit(event: string, data: unknown) {
    this.listeners.get(event)?.forEach((cb) => cb(data));
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.ws = null;
    this.listeners.clear();
  }

  isConnected() {
    return this.ws?.readyState === WebSocket.OPEN;
  }
}

export const wsManager = new WsManager();
