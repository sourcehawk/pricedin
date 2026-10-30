# Build context: the repository root, because the web app installs from the
# workspace lockfile.

FROM node:26.13.0-trixie-slim@sha256:e4db4c914bf8ad241bd24804fc25d272d89bad97e68cdc8a1414862790a02c39 AS base
ENV NEXT_TELEMETRY_DISABLED=1 \
    PNPM_HOME=/pnpm \
    PATH=/pnpm:$PATH
WORKDIR /app
# Node.js 25 and later ship without Corepack, so install the pnpm version
# that the packageManager field of package.json pins.
COPY package.json ./
RUN npm install --global --no-fund --no-audit "$(node -p 'require("./package.json").packageManager')"

FROM base AS deps
COPY pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json web/
COPY e2e/package.json e2e/
RUN --mount=type=cache,id=pnpm-store,target=/pnpm/store \
    pnpm install --frozen-lockfile --filter @pricedin/web --store-dir /pnpm/store

FROM deps AS build
COPY web/ web/
RUN pnpm --filter @pricedin/web build

FROM gcr.io/distroless/nodejs26-debian13:nonroot@sha256:a51c5d631b90e1b298e17a88a307b77f0b959b47de942d033acce0c96be3fb83
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    HOSTNAME=0.0.0.0 \
    PORT=3000
WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /app/web/.next/standalone ./
COPY --from=build --chown=nonroot:nonroot /app/web/.next/static ./web/.next/static
USER nonroot:nonroot
EXPOSE 3000
CMD ["web/server.js"]
