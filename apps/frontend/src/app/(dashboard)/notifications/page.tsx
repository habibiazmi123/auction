import { api } from "@/lib/api";

function timeAgo(date: string) {
  const diff = Date.now() - new Date(date).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "Just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export default async function NotificationsPage() {
  let notifications;
  try {
    notifications = await api.getNotifications();
  } catch {
    return (
      <div className="nm-card text-center">
        <p className="text-[var(--color-text-muted)] mb-4">Failed to load notifications.</p>
        <a href="/notifications" className="nm-btn nm-btn-primary inline-block">Retry</a>
      </div>
    );
  }

  const items = notifications || [];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Notifications</h1>
        <p className="text-[var(--color-text-muted)]">Your activity feed</p>
      </div>

      {items.length === 0 ? (
        <div className="nm-card text-center py-12">
          <p className="text-[var(--color-text-muted)]">No notifications yet.</p>
        </div>
      ) : (
        <div className="space-y-3">
          {items.map((n) => (
            <div key={n.id} className={`nm-card flex items-start gap-3 ${n.read_at ? "opacity-60" : ""}`}>
              {!n.read_at && (
                <span className="w-2 h-2 rounded-full bg-[var(--color-accent)] mt-2 shrink-0" />
              )}
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium">{n.type.replace("_", " ")}</p>
                <p className="text-xs text-[var(--color-text-muted)] mt-1">
                  {JSON.stringify(n.payload).slice(0, 100)}
                </p>
              </div>
              <span className="text-xs text-[var(--color-text-muted)] shrink-0">
                {timeAgo(n.created_at)}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
