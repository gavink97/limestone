FROM caddy:2-builder AS builder
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    xcaddy build \
    --output /usr/bin/caddy \
    --with github.com/gavink97/limestone=.

FROM caddy:2
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
