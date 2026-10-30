"use client";

import Link from "next/link";
import { useId, useState, type SubmitEvent } from "react";
import { ApiError } from "@/api/client";
import { useLoadCompany, useStatus } from "@/api/queries";
import type { IngestRun } from "@/api/types";
import { useStock } from "@/state/StockProvider";

const statusTone: Record<IngestRun["status"], string> = {
  succeeded: "good",
  running: "warn",
  failed: "bad",
};

function when(iso: string | null): string {
  if (iso === null) {
    return "running";
  }
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toISOString().slice(0, 16).replace("T", " ");
}

function stats(s: Record<string, number>): string {
  const parts = Object.entries(s).map(([k, v]) => `${k} ${v.toLocaleString("en-US")}`);
  return parts.length === 0 ? "none" : parts.join(", ");
}

// The ingestion jobs that read new filings for every company. sec-tickers
// reads only the ticker map, and a company load covers one company.
const filingJobs: ReadonlySet<string> = new Set(["sec-bulk", "sec-daily", "daily"]);

/** latestSecRun is the newest successful run of a job that reads filings, the freshness of the filings. */
function latestSecRun(runs: readonly IngestRun[]): IngestRun | undefined {
  return runs
    .filter((r) => filingJobs.has(r.job) && r.status === "succeeded" && r.finishedAt !== null)
    .sort((a, b) => ((a.finishedAt ?? "") < (b.finishedAt ?? "") ? 1 : -1))[0];
}

/**
 * unsettledFilingRuns are the filing jobs whose newest run did not succeed.
 * The status lists only the newest run of each job, so an earlier success
 * of such a job, which can be newer than latestSecRun, is not visible.
 */
function unsettledFilingRuns(runs: readonly IngestRun[]): IngestRun[] {
  return runs.filter((r) => filingJobs.has(r.job) && r.status !== "succeeded");
}

function runState(r: IngestRun): string {
  if (r.abandoned) {
    return "was abandoned";
  }
  return r.status === "running" ? "is still running" : "failed";
}

function LoadForm() {
  const id = useId();
  const [ticker, setTicker] = useState("");
  const load = useLoadCompany();
  const { dispatch } = useStock();
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const t = ticker.trim().toUpperCase();
    if (t !== "") {
      load.mutate(t);
    }
  };
  return (
    <section className="panel" aria-labelledby="load-title">
      <div className="panel-head">
        <div>
          <h2 id="load-title">Load or refresh a company</h2>
          <p>
            Fetches the company&apos;s filings from the SEC and its prices, then derives its
            quarters.
          </p>
        </div>
      </div>
      <form className="load-form" onSubmit={submit}>
        <div className="field">
          <label htmlFor={id}>Ticker</label>
          <input
            id={id}
            value={ticker}
            autoComplete="off"
            spellCheck={false}
            placeholder="AAPL"
            onChange={(e) => {
              setTicker(e.target.value);
            }}
          />
        </div>
        <button type="submit" className="btn" disabled={load.isPending || ticker.trim() === ""}>
          {load.isPending ? "Loading…" : "Load or refresh"}
        </button>
      </form>
      {load.isPending && (
        <p className="note" role="status">
          Loading {load.variables}. This can take a minute.
        </p>
      )}
      {load.isSuccess && (
        <p className="note" role="status">
          {load.variables} is loaded.{" "}
          <Link
            href="/"
            onClick={() => {
              dispatch({ type: "pickStock", ticker: load.variables });
            }}
          >
            Open {load.variables}
          </Link>
        </p>
      )}
      {load.isError && (
        <p className="note down" role="alert">
          {load.variables} could not be loaded.{" "}
          {load.error instanceof ApiError ? load.error.message : "Something went wrong."}
        </p>
      )}
    </section>
  );
}

/** DataPage shows how fresh the stored data is, the latest ingest runs, and loads a company on demand. */
export function DataPage() {
  const status = useStatus();
  const { profile } = useStock();
  const s = status.data;
  const sec = s === undefined ? undefined : latestSecRun(s.ingestRuns);
  const unsettled = s === undefined ? [] : unsettledFilingRuns(s.ingestRuns);
  const stock = profile.data;
  return (
    <main>
      <section className="panel" aria-labelledby="fresh-title">
        <div className="panel-head">
          <div>
            <h2 id="fresh-title">Data freshness</h2>
            <p>The newest data stored for each source, and how many companies it covers.</p>
          </div>
        </div>
        {status.isError && (
          <p className="down" role="alert">
            The status could not be read.{" "}
            {status.error instanceof ApiError ? status.error.message : ""}
          </p>
        )}
        {status.isPending && <p className="muted">Reading the status…</p>}
        {s !== undefined && (
          <div className="data-grid">
            <div className="tile">
              <span className="eyebrow">SEC filings</span>
              <b>{sec === undefined ? "none" : when(sec.finishedAt).slice(0, 10)}</b>
              <span className="small muted">
                {sec === undefined ? "no SEC job has succeeded yet" : `last read by ${sec.job}`}
                {stock?.freshness.latestFiled != null &&
                  `; latest ${stock.ticker.symbol} filing ${stock.freshness.latestFiled}`}
              </span>
              {unsettled.map((r) => (
                <span key={r.id} className="small warnc" style={{ display: "block" }}>
                  The newest {r.job} run {runState(r)}, so the filings can be newer than this date
                  shows.
                </span>
              ))}
            </div>
            <div className="tile">
              <span className="eyebrow">Prices</span>
              <b>{s.latestPriceDate ?? "none"}</b>
              <span className="small muted">latest close</span>
            </div>
            <div className="tile">
              <span className="eyebrow">Companies</span>
              <b>{s.companies.withFundamentals.toLocaleString("en-US")}</b>
              <span className="small muted">
                with quarters, of {s.companies.total.toLocaleString("en-US")} known (
                {s.companies.withFacts.toLocaleString("en-US")} with facts)
              </span>
            </div>
            <div className="tile">
              <span className="eyebrow">SEC retries</span>
              <b>{s.secRetries.waiting.toLocaleString("en-US")}</b>
              <span className="small muted">
                {s.secRetries.waiting === 1 ? "company waits" : "companies wait"} for another try;{" "}
                {s.secRetries.exhausted.toLocaleString("en-US")} failed too often and{" "}
                {s.secRetries.exhausted === 1 ? "is" : "are"} no longer tried
              </span>
            </div>
            <div className="tile">
              <span className="eyebrow">Tracked</span>
              <b>{s.trackedCompanies.toLocaleString("en-US")}</b>
              <span className="small muted">
                {s.trackedCompanies === 1 ? "company" : "companies"} loaded on demand, kept current
                by the daily jobs
              </span>
            </div>
          </div>
        )}
      </section>
      <section className="panel" aria-labelledby="runs-title">
        <div className="panel-head">
          <div>
            <h2 id="runs-title">Latest ingest runs</h2>
            <p>The last run of each job, with the rows it reported.</p>
          </div>
        </div>
        {s !== undefined && s.ingestRuns.length === 0 && (
          <p className="muted">No job has run yet.</p>
        )}
        {s !== undefined && s.ingestRuns.length > 0 && (
          <div className="runs-wrap">
            <table className="runs">
              <thead>
                <tr>
                  <th scope="col">Job</th>
                  <th scope="col">Status</th>
                  <th scope="col">Started (UTC)</th>
                  <th scope="col">Finished (UTC)</th>
                  <th scope="col">Counts</th>
                </tr>
              </thead>
              <tbody>
                {s.ingestRuns.map((r) => (
                  <tr key={r.id}>
                    <th scope="row" className="mono">
                      {r.job}
                    </th>
                    <td>
                      <span className={`pill ${statusTone[r.status]}`}>
                        {r.abandoned ? "abandoned" : r.status}
                      </span>
                      {r.error !== "" && <div className="err">{r.error}</div>}
                    </td>
                    <td className="mono">{when(r.startedAt)}</td>
                    <td className="mono">{when(r.finishedAt)}</td>
                    <td className="counts">{stats(r.stats)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <LoadForm />
    </main>
  );
}
