# syntax=docker/dockerfile:1
# pingtxt hub: Go API + scheduler + embedded Next.js UI in one small image.

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /web/out ./internal/ui/dist
ARG VERSION=dev
ARG TARGETOS TARGETARCH
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/pingtxt ./cmd/pingtxt
# Agent binaries for every platform, served by the hub at /download/* for the install script.
RUN for p in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64; do \
      os=${p%/*}; arch=${p#*/}; \
      GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
        -o /out/agents/pingtxt-agent-$os-$arch ./cmd/pingtxt-agent || exit 1; \
    done

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=go /out/pingtxt /usr/local/bin/pingtxt
COPY --from=go /out/agents /usr/share/pingtxt/agents
ENV PINGTXT_ADDR=:8090 \
    PINGTXT_DATA=/data \
    PINGTXT_AGENT_DIR=/usr/share/pingtxt/agents
VOLUME /data
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8090/api/health >/dev/null || exit 1
ENTRYPOINT ["pingtxt"]
