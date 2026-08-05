import type { Metadata } from "next";
import { Inter } from "next/font/google";
import { Toaster } from "sonner";
import "./globals.css";

const inter = Inter({ subsets: ["latin"] });

export const metadata: Metadata = {
  title: "Auction Platform",
  description: "Real-time auction platform",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body
        className={`${inter.className} bg-[var(--color-bg)] text-[var(--color-text)] min-h-screen antialiased`}
      >
        {children}
        <Toaster position="top-right" />
      </body>
    </html>
  );
}
