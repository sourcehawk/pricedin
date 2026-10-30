"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useId, useState, type SubmitEvent } from "react";
import { ApiError } from "@/api/client";
import { useApi } from "@/api/queries";
import { safeNext } from "@/api/session";
import { Brand } from "@/components/Brand";

function failure(e: unknown): string {
  if (!(e instanceof ApiError)) {
    return "Something went wrong. Try again.";
  }
  switch (e.status) {
    case 0:
      return "The server cannot be reached. Check your connection and try again.";
    case 401:
      return "The email or the password is wrong.";
    case 503:
      return e.problem?.type === "urn:pricedin:problem:login-disabled"
        ? "Login is turned off: the server has no owner account. Set PRICEDIN_OWNER_EMAIL and PRICEDIN_OWNER_PASSWORD_HASH on the server, then restart it."
        : `The login failed: ${e.message}`;
    case 429:
      return e.retryAfter === null
        ? "Too many attempts. Wait a minute, then try again."
        : `Too many attempts. Try again in ${String(e.retryAfter)} seconds.`;
    default:
      return `The login failed: ${e.message}`;
  }
}

/** LoginForm logs the owner in, then opens the page in ?next= or the home page. */
export function LoginForm() {
  const api = useApi();
  const router = useRouter();
  const params = useSearchParams();
  const next = safeNext(params.get("next"));
  const expired = params.get("expired") === "1";
  const emailId = useId();
  const passwordId = useId();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.login(email, password);
      router.replace(next);
    } catch (err) {
      setError(failure(err));
      setBusy(false);
    }
  };

  return (
    <div className="login card">
      <Brand />
      <p className="lead">
        See what growth a stock price already assumes before you start your research.
      </p>
      {expired && error === null && (
        <p className="form-note" role="status">
          Your session ended. Log in again.
        </p>
      )}
      {error !== null && (
        <p className="form-note error" role="alert">
          {error}
        </p>
      )}
      <form
        onSubmit={(e) => {
          void submit(e);
        }}
      >
        <div className="field">
          <label htmlFor={emailId}>Email</label>
          <input
            id={emailId}
            type="email"
            autoComplete="username"
            required
            value={email}
            onChange={(e) => {
              setEmail(e.target.value);
            }}
          />
        </div>
        <div className="field">
          <label htmlFor={passwordId}>Password</label>
          <input
            id={passwordId}
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => {
              setPassword(e.target.value);
            }}
          />
        </div>
        <button type="submit" className="btn" disabled={busy}>
          {busy ? "Logging in…" : "Log in"}
        </button>
      </form>
    </div>
  );
}
