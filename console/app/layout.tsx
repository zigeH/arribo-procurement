import type { Metadata } from "next";
import { Suspense } from "react";
import "./globals.css";
import { ApiStatusBanner } from "@/components/ApiStatusBanner";
import { AsOfPicker } from "@/components/AsOfPicker";
import { Nav } from "@/components/Nav";

export const metadata: Metadata = {
  title: "Arribo",
  description: "Procurement execution console",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body className="antialiased">
        <div className="flex min-h-screen">
          <aside className="w-56 shrink-0 border-r border-zinc-200 bg-zinc-50 px-4 py-6">
            <div className="mb-8 px-2 text-sm font-semibold tracking-tight">Arribo</div>
            <Suspense>
              <Nav />
            </Suspense>
          </aside>
          <div className="flex min-w-0 flex-1 flex-col">
            <header className="flex h-14 items-center justify-end border-b border-zinc-200 px-8">
              <Suspense>
                <AsOfPicker />
              </Suspense>
            </header>
            <ApiStatusBanner />
            <main className="flex-1 px-8 py-8">{children}</main>
          </div>
        </div>
      </body>
    </html>
  );
}
