VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 linux/arm darwin/arm64 darwin/amd64

.PHONY: all web hub agent agents dev-hub dev-web test lint clean docker

all: web hub agent

web:
	cd web && npm ci && npm run build
	rm -rf internal/ui/dist && mkdir -p internal/ui/dist && cp -r web/out/. internal/ui/dist/ && touch internal/ui/dist/.gitkeep

hub:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pingtxt ./cmd/pingtxt

agent:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/pingtxt-agent ./cmd/pingtxt-agent

# Cross-compiled agents, served by the hub at /download/<name>.
agents:
	@mkdir -p dist/agents
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building agent $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/agents/pingtxt-agent-$$os-$$arch ./cmd/pingtxt-agent || exit 1; \
	done

dev-hub:
	PINGTXT_DATA=./data go run ./cmd/pingtxt

dev-web:
	cd web && npm run dev

test:
	go test ./cmd/... ./internal/...

lint:
	go vet ./cmd/... ./internal/...
	cd web && npm run lint && npm run typecheck

docker:
	docker build -t pingtxt:dev .
	docker build -f Dockerfile.agent -t pingtxt-agent:dev .

clean:
	rm -rf dist web/out web/.next
	find internal/ui/dist -mindepth 1 ! -name .gitkeep -delete
