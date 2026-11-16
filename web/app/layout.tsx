import type { Metadata, Viewport } from "next";
import "./globals.css";
import { ServiceWorkerRegister } from "@/components/ServiceWorkerRegister";
import { DesktopTitleBar } from "@/components/DesktopTitleBar";
import { ClientErrorReporter } from "@/components/ClientErrorReporter";
import { LanguageProvider } from "@/components/LanguageProvider";

import { ThemeProvider } from "@/lib/theme";
import { ToastProvider } from "@/components/ui/Toast";

export const metadata: Metadata = {
  title: "superbot",
  description: "自托管 AI 同事团队 — 与你的 Bot 一起工作。",
};

export const viewport: Viewport = {
  themeColor: "#fcfcfc",
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <head>
        <script
          dangerouslySetInnerHTML={{
            __html: `try{
var m=localStorage.getItem('superbot-theme')||'light';
var d=m==='system'?(window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'):m;
document.documentElement.dataset.theme=d;
var colors={green:['#0d9655','#0b8550','#10ab61'],blue:['#1680f3','#126fd4','#1680f3'],violet:['#8351e4','#7442d1','#8351e4'],orange:['#ee620d','#d2550a','#ee620d'],black:['#1c1c1c','#333333','#1c1c1c']};
var a=localStorage.getItem('superbot-accent')||'green';
var p=colors[a]||colors.green;
document.documentElement.style.setProperty('--sb-user-bubble',p[0]);
document.documentElement.style.setProperty('--sb-user-bubble-hover',p[1]);
document.documentElement.style.setProperty('--sb-success',p[2]);
}catch(e){}`,
          }}
        />
      </head>
      <body>
        <DesktopTitleBar />
        <ServiceWorkerRegister />
        <ClientErrorReporter />
        <LanguageProvider>
          <ThemeProvider>
            <ToastProvider>{children}</ToastProvider>
          </ThemeProvider>
        </LanguageProvider>
      </body>
    </html>
  );
}
