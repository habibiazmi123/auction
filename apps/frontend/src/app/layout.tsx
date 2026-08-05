import type { Metadata } from "next";
import { Inter, Geist } from "next/font/google";
import { Toaster } from "sonner";
import "./globals.css";
import { cn } from "@/lib/utils";

const geist = Geist({subsets:['latin'],variable:'--font-sans'});

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
    <html lang="en" className={cn("font-sans", geist.variable)}>
      <body
        className={`${inter.className} bg-[var(--color-bg)] text-[var(--color-text)] min-h-screen antialiased`}
      >
        {children}
        <Toaster position="top-right" />
      </body>
    </html>
  );
}
