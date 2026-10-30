import type { ReactNode } from "react";
import { AppProviders } from "@/components/shell/Providers";
import { TopBar } from "@/components/shell/TopBar";

export default function AppLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <AppProviders>
      <div className="wrap">
        <TopBar />
        {children}
      </div>
    </AppProviders>
  );
}
