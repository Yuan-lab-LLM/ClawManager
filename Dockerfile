FROM --platform=$BUILDPLATFORM node:24-alpine AS hermes-desktop-deps

WORKDIR /app
RUN apk add --no-cache git
COPY hermes-desktop-web/package*.json ./hermes-desktop-web/
RUN --mount=type=cache,id=clawmanager-hermes-npm,target=/root/.npm,sharing=locked npm ci --prefix hermes-desktop-web --ignore-scripts

FROM hermes-desktop-deps AS hermes-desktop-builder
COPY hermes-desktop-web/ ./hermes-desktop-web/
RUN node hermes-desktop-web/scripts/build.mjs

FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.1-alpine AS backend-builder

WORKDIR /app/backend

ARG TARGETOS
ARG TARGETARCH

ARG GOPROXY
ARG GOSUMDB
ARG GOFLAGS
ENV GOFLAGS=${GOFLAGS}

RUN apk add --no-cache git

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -buildvcs=false \
    -ldflags="-s -w -buildid= -X clawreef/internal/buildinfo.Version=${VERSION} -X clawreef/internal/buildinfo.Commit=${VCS_REF} -X clawreef/internal/buildinfo.BuildTime=${BUILD_DATE}" \
    -o /out/clawreef-server ./cmd/server

FROM nginx:1.27-alpine

# nginx-module-njs provides ngx_http_js_module.so, loaded by nginx.conf to run
# the desktop access-token verification (deployments/nginx/njs/desktop_auth.js).
RUN apk add --no-cache dumb-init openssl nginx-module-njs

WORKDIR /app

COPY --from=backend-builder /out/clawreef-server /usr/local/bin/clawreef-server
COPY --from=frontend-builder /app/frontend/dist /usr/share/nginx/html
COPY --from=hermes-desktop-builder /app/frontend/public/hermes-desktop-web /usr/share/nginx/html/hermes-desktop-web
COPY deployments/nginx/nginx.conf /etc/nginx/nginx.conf
COPY deployments/nginx/njs/desktop_auth.js /etc/nginx/njs/desktop_auth.js
COPY deployments/container/start.sh /app/start.sh

RUN chmod +x /app/start.sh \
    && mkdir -p /etc/nginx/tls /var/log/clawreef

EXPOSE 8443

ENTRYPOINT ["dumb-init", "--"]
CMD ["/app/start.sh"]
