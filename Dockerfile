FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

FROM golang:1.26.1-alpine AS backend-builder

WORKDIR /app/backend

RUN apk add --no-cache git

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /out/clawreef-server ./cmd/server

FROM nginx:1.27-alpine

RUN apk add --no-cache dumb-init openssl

WORKDIR /app

COPY --from=backend-builder /out/clawreef-server /usr/local/bin/clawreef-server
COPY --from=frontend-builder /app/frontend/dist /usr/share/nginx/html
COPY deployments/nginx/nginx.conf /etc/nginx/nginx.conf
COPY deployments/container/start.sh /app/start.sh

RUN chmod +x /app/start.sh \
    && mkdir -p /etc/nginx/tls /var/log/clawreef

EXPOSE 8443

ENTRYPOINT ["dumb-init", "--"]
CMD ["/app/start.sh"]
