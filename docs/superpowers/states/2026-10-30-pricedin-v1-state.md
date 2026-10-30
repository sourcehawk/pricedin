---
feature: pricedin-v1
spec: docs/superpowers/specs/2026-10-30-pricedin-v1-design.md
plan: docs/superpowers/plans/2026-10-30-pricedin-v1-plan.md
tracking_issue: "#1"
sub_pr_approval: autonomous
sub_pr_review_loop: on
sub_pr_target: main
status: consumer-wave
---

# PricedIn v1 — orchestration state

## Phases

- **Wave 0** — planning docs (no issue)
- **Wave 1 (foundational)** — `#2`
- **Wave 2** — `#3`, `#4`, `#5`, `#6`
- **Wave 3** — `#7`, `#8`
- **Wave 4** — `#9`, `#10`
- **Wave 5** — `#11`, `#12`
- **Wave 6 (consumers)** — `#13`, `#14`, `#15`, `#16`
- **Wave 7** — `#17`

## PRs / worktrees

| Issue | Branch | Worktree path | PR (→ base) | Status |
| --- | --- | --- | --- | --- |
| planning | docs/pricedin-v1-plan | (main checkout) | #18 → main | self-merged |
| #2 | chore/project-foundation | (removed) | #19 → main | self-merged |
| #3 | feat/go-engine | (removed) | #21 → main | self-merged |
| #4 | feat/database-store | (removed) | #23 → main | self-merged |
| #5 | feat/sec-client | (removed) | #20 → main | self-merged |
| #6 | feat/massive-prices | (removed) | #22 → main | self-merged |
| #7 | feat/ts-engine | (removed) | #25 → main | self-merged |
| #8 | feat/quarter-derivation | (removed) | #24 → main | self-merged |
| #9 | feat/ingestion-jobs | (removed) | #27 → main | self-merged |
| #10 | feat/http-api | (removed) | #28 → main | self-merged |
| #11 | feat/web-shell | .claude/worktrees/web-shell | → main | in-progress |
| #12 | chore/deploy-and-docs | .claude/worktrees/deploy-and-docs | → main | in-progress |
| #13 | feat/priced-in-panel | .claude/worktrees/priced-in-panel | → main | not-started |
| #14 | feat/price-chart | .claude/worktrees/price-chart | → main | not-started |
| #15 | feat/earnings-panels | .claude/worktrees/earnings-panels | → main | not-started |
| #16 | feat/financials-panel | .claude/worktrees/financials-panel | → main | not-started |
| #29 | fix/engine-price-bound | .claude/worktrees/price-bound | → main | in-progress |
| #17 | test/end-to-end-flows | .claude/worktrees/end-to-end-flows | → main | not-started |

## Contracts

| Name | Realization | Realized in | Status |
| --- | --- | --- | --- |
| `domain-types` | pre-merge stub (inside #2) | #19 | locked |
| `engine-io` | data-only (cases file) | #21 | locked |
| `engine-cases` | data-only | #21 | locked |
| `raw-recorder` | data-only | #20, #22 | locked |
| `price-provider` | data-only (sequential) | #22 | locked |
| `fundamentals-series` | data-only (sequential) | #24 | locked |
| `openapi` | data-only (sequential) | #28 | locked |
| `walkthrough-api` | data-only (sequential) | #11 | pending |
| `fixture-server-paths` | data-only | #27 | locked |

## Bubble-up log

- 2026-10-30: #25 (TS engine), #27 (ingestion) and #28 (API) merged. Reviews found: advisory locks behind a transaction pooler (now a startup check plus PRICEDIN_DATABASE_LOCK_URL), a frozen SEC cursor (retry table), stale on-demand companies (tracked_companies), abandoned runs, recorder dedup defeated by request_id, sessions surviving a password change (owner fingerprint), the login limit behind proxies (trusted hops, IPv6 /64), dividend provenance. SEC spelling for symbols everywhere. The API keeps per-request Derive (stored rows lack the confirmed split list). Follow-up #29 raises the engine price bound (BRK.A).
- 2026-10-30: #21 (engine) and #24 (fundamentals) merged. Independent reviews caught real defects: Inf in JSON, Go/oracle drift on non-positive exit P/E, NVDA Q4 FY23 EPS on mixed split bases, mis-scaled share counts. The Copilot loop on #21 kept finding new edges; after the cap the orchestrator merged on green CI with the last finding fixed. #9 and #10 dispatched in parallel; their shared contract is the `CompanyLoader` interface.
- 2026-10-30: #20, #22 and #23 merged after Copilot loops and independent reviews. Decisions: SEC calls run sequentially per process; the SEC bulk job fetches submissions through the API instead of submissions.zip (255 MB central directory); price fixtures shrank to 28 files (grouped window plus aggs) and get SEC-anchored prices in #9; `prices verify` prints no provider values; prices_daily stores raw closes only; production runs as a non-owner Postgres role (#12). Session notes for later PRs live in the scratchpad file notes-for-later.md.
- 2026-10-30: #19 merged (946668d). A `main` ruleset now requires the `ci-ok` check, blocks force-pushes and deletion, and runs Copilot code review on every push, so the review loop waits for the push-triggered review instead of re-requesting.
- 2026-10-30: Wave 2 (#3-#6) started before #19 merged, from foundation head 140abf3. Each agent waits for #19 on main, then runs `git rebase --onto origin/main 140abf3` before opening its PR.
- 2026-10-30: Copilot review of the planning PR (#18) added the spec section "Corrections to the prototype" (median, exact-baseline grid row, chart scales, band hover layering, scenario walkthrough), the `raw-recorder` contract with dated, hashed keys, the duration-weighted Q4 share rule, explicit dated split evidence, and the narrowed Origin check. Wave-2 briefs carry them.
- 2026-10-30: Stooq requires a CAPTCHA-gated key and a JS browser check. The owner chose Massive Stocks Starter. Spec, plan and `CLAUDE.md` updated. Do not bypass Stooq's check.
- 2026-10-30: The owner is away and wants no blocking questions. Decide and record decisions here.

## Pending snapshot

1. #11 web shell (agent wiring the real API client), #12 deploy and docs, #29 price bound: in progress in their worktrees.
2. After #11 merges: dispatch #13-#16 panels in parallel (shared brief: scratchpad brief-panels.md), then #17 e2e (brief-17.md).
3. At the end: a checkpoint review across the union, refresh the README screenshot, tear down plan and state per developing-a-feature Step 7 (the spec stays; the repo has no ADR folder), and report to the owner.

## Resume checklist

For a fresh Claude session resuming this work: invoke `feature-dev-workflow:resuming-a-feature` — it executes the steps below, routes by the `status:` frontmatter, and works the `## Pending snapshot`. Fallback if that skill is unavailable:

1. Read this state file in full.
2. Read the plan at the path in the `plan:` frontmatter.
3. Read the spec at the path in the `spec:` frontmatter.
4. Verify each open PR's actual state via `gh pr view <num>`.
5. For each `in-progress` or `draft` row, `cd` to the worktree path and check `git status -sb`.
6. Re-dispatch subagents as needed per `feature-dev-workflow:developing-a-feature`, or work the `## Pending snapshot`.
