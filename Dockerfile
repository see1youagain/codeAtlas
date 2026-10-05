FROM node:24-alpine AS frontend
RUN corepack enable
WORKDIR /src
COPY web/package.json web/pnpm-lock.yaml ./web/
RUN pnpm --dir web install --frozen-lockfile
COPY web ./web
RUN pnpm --dir web build

FROM golang:1.25-alpine AS backend
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=frontend /src/internal/server/web/dist ./internal/server/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/codeatlas ./cmd/server

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app && apk add --no-cache ca-certificates git
WORKDIR /app
COPY --from=backend /out/codeatlas /app/codeatlas
RUN mkdir -p /data/runtime && chown -R app:app /data
USER app
EXPOSE 8080
ENTRYPOINT ["/app/codeatlas"]
