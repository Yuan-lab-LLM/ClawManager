ARG BASE_IMAGE=clawmanager:opencode-workspace-root-fix-20260825
FROM golang:1.26-bookworm AS agent-builder
WORKDIR /src
COPY clawmanager-agent/ ./
RUN go test ./internal/runtime/opencode/... \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.version=opencode-v2-20261012" -o /out/clawmanager-agent ./cmd/clawmanager-agent

FROM ${BASE_IMAGE}
ENV OPENCODE_RUNTIME_MAJOR=2
COPY package/bin/opencode /usr/local/bin/opencode
COPY --from=agent-builder /out/clawmanager-agent /usr/local/bin/clawmanager-agent
RUN chmod 755 /usr/local/bin/opencode /usr/local/bin/clawmanager-agent \
    && /usr/local/bin/opencode --version | grep -Fx 'opencode v2.0.27'
