import { Navbar } from "@/components/layout/navbar";
import { Sidebar } from "@/components/layout/sidebar";

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-[var(--color-bg)]">
      <Navbar />
      <Sidebar />
      <main className="pt-24 pl-64 pr-4 pb-8">
        {children}
      </main>
    </div>
  );
}
