# syntax=docker/dockerfile:1
# pingtxt hub: Go API + scheduler + embedded Next.js UI in one small image.

# Keep in sync with the "go" line of go.mod (CI checks it). The agent binaries
# bundled here must be byte-identical to the ones in the GitHub release, so
# agents can check an update against the published SHA256SUMS.
ARG GO_VERSION=1.26.10

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# Agent binaries for every platform, served by the hub at /download/* for the
# install script and self-update. release.yml exports the "agents" stage too
# and checks it matches the release binaries.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS agents-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN for p in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64; do \
      os=${p%/*}; arch=${p#*/}; \
      GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$VERSION" \
        -o /out/pingtxt-agent-$os-$arch ./cmd/pingtxt-agent || exit 1; \
    done

FROM scratch AS agents
COPY --from=agents-build /out /

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /web/out ./internal/ui/dist
ARG VERSION=dev
ARG TARGETOS TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$VERSION" -o /out/pingtxt ./cmd/pingtxt

FROM alpine:3.22
# The hub runs as user pingtxt (uid 10001): docker/entrypoint.sh starts as
# root, fixes the owner of /data and drops privileges.
RUN apk add --no-cache ca-certificates tzdata setpriv \
 && addgroup -S -g 10001 pingtxt \
 && adduser -S -D -H -u 10001 -G pingtxt -h /data -s /sbin/nologin pingtxt
COPY --from=go /out/pingtxt /usr/local/bin/pingtxt
COPY --from=agents / /usr/share/pingtxt/agents
ENV PINGTXT_ADDR=:8090 \
    PINGTXT_DATA=/data \
    PINGTXT_AGENT_DIR=/usr/share/pingtxt/agents
VOLUME /data
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8090/api/health >/dev/null || exit 1
COPY docker/entrypoint.sh /usr/local/bin/docker-entrypoint
ENTRYPOINT ["docker-entrypoint", "pingtxt"]
