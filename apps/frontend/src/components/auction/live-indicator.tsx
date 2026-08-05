"use client";

export function LiveIndicator({ connected }: { connected: boolean }) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <span
        className={`w-2 h-2 rounded-full ${
          connected ? "bg-green-500 animate-pulse" : "bg-[var(--color-accent)]"
        }`}
      />
      <span className="text-[var(--color-text-muted)]">
        {connected ? "Live" : "Disconnected"}
      </span>
    </div>
  );
}
