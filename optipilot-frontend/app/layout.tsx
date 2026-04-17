import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { WsProvider } from "../lib/ws";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "OptiPilot Dashboard",
  description: "Grafana-inspired predictive autoscaling dashboard for OptiPilot",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${geistSans.variable} ${geistMono.variable} h-full antialiased dark`}
    >
      <body className="min-h-full flex flex-row bg-zinc-950 text-foreground">
        <WsProvider>
          <div className="w-64 border-r border-border p-4 shrink-0 bg-background flex flex-col pt-8">
            <h1 className="text-xl font-bold mb-8 text-primary px-2">OptiPilot</h1>
            <nav className="space-y-2 text-sm text-muted-foreground flex-1">
              <a href="/" className="block p-2 rounded hover:bg-accent hover:text-accent-foreground transition-colors">Dashboard</a>
              <a href="/audit" className="block p-2 rounded hover:bg-accent hover:text-accent-foreground transition-colors">Audit Log</a>
              <a href="/settings" className="block p-2 rounded hover:bg-accent hover:text-accent-foreground transition-colors">Settings</a>
            </nav>
            <div className="text-xs text-muted-foreground p-2">v0.1.0</div>
          </div>
          <main className="flex-1 overflow-y-auto bg-background">{children}</main>
        </WsProvider>
      </body>
    </html>
  );
}
