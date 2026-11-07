import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "@xterm/xterm/css/xterm.css";
import "./globals.css";
import { Sidebar } from "@/components/sidebar";
import { ConfirmProvider } from "@/components/confirm-dialog";
import { TerminalHost } from "@/components/terminal-host";
import { TooltipProvider } from "@/components/ui/tooltip";
import { CommandPalette } from "@/components/command-palette";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "TrueDeploy",
  description: "Set up servers, repos and Docker deployments over SSH",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`dark ${geistSans.variable} ${geistMono.variable} h-full antialiased`}>
      <body className="flex h-full font-sans">
        <TooltipProvider delayDuration={300}>
          <ConfirmProvider>
            <Sidebar />
            <main className="relative flex-1 overflow-y-auto">
              {children}
              <TerminalHost />
            </main>
            <CommandPalette />
          </ConfirmProvider>
        </TooltipProvider>
      </body>
    </html>
  );
}
