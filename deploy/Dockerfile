# syntax=docker/dockerfile:1

FROM node:22-alpine3.24 AS ui-build

WORKDIR /src/ui/admin
RUN npm install --global pnpm@12.18.0
COPY ui/admin/package.json ui/admin/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY ui/admin/ ./
RUN pnpm build

FROM golang:1.26.11-alpine3.24 AS go-build

RUN apk add --no-cache build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=ui-build /src/ui/admin/dist ./ui/admin/dist
RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/jasta ./cmd/jasta

FROM alpine:3.24 AS runtime

RUN apk add --no-cache ca-certificates libgcc su-exec tzdata \
    && addgroup -S -g 10001 jasta-content \
    && addgroup -S -g 10002 jasta-admin \
    && adduser -S -D -H -u 10001 -G jasta-content jasta-web \
    && adduser -S -D -H -u 10002 -G jasta-admin jasta-admin \
    && addgroup jasta-admin jasta-content \
    && mkdir -p /etc/jasta/websites /opt/jasta/www /var/lib/jasta /run/jasta \
    && chown jasta-admin:jasta-content /var/lib/jasta \
    && chmod 0750 /var/lib/jasta \
    && chown jasta-web:jasta-content /run/jasta \
    && chmod 2770 /run/jasta

COPY --from=go-build --chown=root:root /out/jasta /usr/local/bin/jasta
COPY --chmod=0555 deploy/entrypoint.sh /usr/local/bin/jasta-entrypoint
COPY --chown=root:root deploy/config.yaml /etc/jasta/config.yaml
COPY --chown=root:root deploy/admin.yaml /etc/jasta/admin.yaml
COPY --chown=root:root deploy/websites/ /etc/jasta/websites/
COPY --chown=root:root deploy/www/ /opt/jasta/www/

EXPOSE 15432 15433
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/jasta-entrypoint"]
CMD ["web"]
