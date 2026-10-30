"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError } from "@/api/client";
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
  const [error, setError] = useState<string | null>(null);
  const done = () => {
    cache.clear();
    router.replace("/login");
  };
  return (
    <>
      <button
        type="button"
        disabled={busy}
        onClick={() => {
          setBusy(true);
          setError(null);
          api.logout().then(done, (e: unknown) => {
            // A 401 means the session is already gone: that is a logout too.
            if (e instanceof ApiError && e.status === 401) {
              done();
              return;
            }
            setError(`Logout failed. ${e instanceof ApiError ? e.message : "Try again."}`);
            setBusy(false);
          });
        }}
      >
        Log out
      </button>
      {error !== null && (
        <span className="small down" role="alert">
          {error}
        </span>
      )}
    </>
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
  const price = state.price ?? p?.latestClose?.value ?? null;
  const eps = state.ttmEps ?? p?.ttmEps?.value ?? null;
  // A null P/E from the engine means that the EPS in use is not positive.
  const pe =
    evaluation !== null ? evaluation.peNow.value : levers === null ? (p?.pe?.value ?? null) : null;
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
