# syntax=docker/dockerfile:1
# Multi-stage build: SvelteKit SPA -> embedded in a static Go binary -> distroless.

FROM node:24-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
COPY api/openapi.yaml /src/api/openapi.yaml
COPY testdata/ /src/testdata/
RUN npm run build

FROM golang:1.27-bookworm AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/build ./internal/web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/blair ./cmd/blair

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/blair /blair
ENV BLAIR_ADDR=:8080 BLAIR_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/blair"]
CMD ["serve"]
