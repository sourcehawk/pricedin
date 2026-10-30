"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useApi } from "@/api/queries";
import { times, usd } from "@/components/format";
import { Sourced } from "@/components/Sourced";
import { InfoTip } from "@/components/tip/InfoTip";
import { useStock } from "@/state/StockProvider";

function LogoutButton() {
  const api = useApi();
  const router = useRouter();
  const cache = useQueryClient();
  const [busy, setBusy] = useState(false);
  return (
    <button
      type="button"
      disabled={busy}
      onClick={() => {
        setBusy(true);
        // The login page is the end state even when the call fails: the
        // session is then gone or unusable anyway.
        api
          .logout()
          .catch(() => undefined)
          .finally(() => {
            cache.clear();
            router.replace("/login");
          });
      }}
    >
      Log out
    </button>
  );
}

/**
 * TopBar shows the brand, the selected company with its price, TTM EPS,
 * P/E and close date, and the links to the analysis and the data page.
 */
export function TopBar() {
  const { state, profile, levers, evaluation, provenance } = useStock();
  const path = usePathname();
  const p = profile.data;
  const price = levers?.price ?? p?.latestClose?.value ?? null;
  const eps = levers?.ttmEps ?? p?.ttmEps?.value ?? null;
  const pe = evaluation?.peNow.value ?? p?.pe?.value ?? null;
  return (
    <header className="top">
      <div className="brand">
        <h1>
          <Link href="/">
            Priced<span>In</span>
          </Link>
        </h1>
      </div>
      <div className="picker">
        {p !== undefined && (
          <div className="co">
            <b>{p.company.name}</b>
            <span className="muted small">
              {p.company.sicDescription === ""
                ? p.ticker.symbol
                : `${p.company.sicDescription} · ${p.ticker.symbol}`}
            </span>
          </div>
        )}
        {p === undefined && state.ticker !== null && <span className="muted">{state.ticker}</span>}
      </div>
      {p !== undefined && (
        <>
          <div className="stat">
            <span className="eyebrow">Price</span>
            <b>
              <Sourced value={p.latestClose} user={provenance.price?.kind === "user"}>
                {usd(price)}
              </Sourced>
            </b>
          </div>
          <div className="stat">
            <span className="eyebrow">
              TTM EPS
              <InfoTip label="About TTM EPS">
                Trailing twelve months: diluted EPS of the last four reported quarters added up. A
                full year removes seasonal swings.
              </InfoTip>
            </span>
            <b>
              <Sourced value={p.ttmEps} user={provenance.ttmEps?.kind === "user"}>
                {usd(eps)}
              </Sourced>
            </b>
          </div>
          <div className="stat">
            <span className="eyebrow">
              P/E
              <InfoTip label="About P/E">
                Share price divided by TTM EPS. The number of years of current earnings that the
                price pays for.
              </InfoTip>
            </span>
            <b>{times(pe)}</b>
          </div>
          <div className="stat">
            <span className="eyebrow">As of close</span>
            <b className="small">{p.latestClose?.asOf ?? "n/a"}</b>
          </div>
        </>
      )}
      <nav className="top-nav" aria-label="Pages">
        <Link href="/" aria-current={path === "/" ? "page" : undefined}>
          Analysis
        </Link>
        <Link href="/data" aria-current={path === "/data" ? "page" : undefined}>
          Data
        </Link>
        <LogoutButton />
      </nav>
    </header>
  );
}
