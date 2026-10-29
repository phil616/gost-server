# syntax=docker/dockerfile:1
ARG GO_IMAGE=golang:1.27.3-bookworm
ARG NODE_IMAGE=node:24-bookworm-slim
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian13:nonroot

FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN npm ci --no-audit --no-fund
COPY web/.umirc.ts web/tsconfig.json web/license-manifest-plugin.cjs ./
COPY web/src ./src
ARG VERSION=dev
ENV ALTASCI_VERSION=$VERSION
RUN npm run typecheck && npm test && npm run build

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS server-build
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/gost-x ./third_party/gost-x
RUN go mod download
COPY . .
RUN go test -mod=readonly ./internal/host ./cmd/altasci-server
ARG TARGETOS
ARG TARGETARCH
RUN mkdir -p /out /image-data /image-run && chmod 0700 /image-data /image-run && \
    for app in altasci-control altasci-edge altasci-server; do \
      CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
        go build -mod=readonly -trimpath -ldflags='-s -w' -o /out/$app ./cmd/$app || exit 1; \
    done
RUN tar -czf /out/project-source.tar.gz \
    cmd internal third_party web scripts deploy docs patches .github \
    go.mod go.sum Dockerfile compose.yaml compose.env.example .dockerignore \
    LICENSE THIRD_PARTY_NOTICES.md README.md CONTRIBUTING.md SECURITY.md

FROM server-build AS legal
COPY --from=web-build /usr/local/bin/node /usr/local/bin/node
COPY --from=web-build /src/web/node_modules /src/web/node_modules
COPY --from=web-build /src/web/dist /src/web/dist
RUN node scripts/collect-licenses.cjs /out/licenses /src/web && \
    node scripts/verify-licenses.cjs /out/licenses

FROM ${RUNTIME_IMAGE} AS runtime
ARG VERSION=dev
ARG VCS_REF=unknown
LABEL org.opencontainers.image.title="AltasCI Tunnel Server" \
      org.opencontainers.image.source="https://github.com/phil616/gost-server" \
      org.opencontainers.image.authors="@phil616" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$VCS_REF
COPY --from=server-build /out/altasci-control /out/altasci-edge /out/altasci-server /usr/local/bin/
COPY scripts/init-docker.sh scripts/deploy-docker.sh /usr/share/altasci/deploy/
COPY --from=web-build /src/web/dist /app/web/dist
COPY --from=legal /out/licenses /usr/share/altasci/licenses
COPY --from=server-build /out/project-source.tar.gz /usr/share/altasci/sources/project-source.tar.gz
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/altasci/
COPY --from=server-build --chown=65532:65532 /image-data /data
COPY --from=server-build --chown=65532:65532 /image-run /run/altasci
USER 65532:65532
WORKDIR /app
VOLUME ["/data"]
EXPOSE 18080 18081
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=15s --timeout=7s --start-period=30s --retries=3 \
  CMD ["/usr/local/bin/altasci-server", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/altasci-server"]
CMD ["serve"]
