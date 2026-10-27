# Parachute Health build of policy-bot for the org image-build pipeline, which
# runs a plain `docker build` on this file. Upstream builds its image with
# godel (docker/Dockerfile); this reproduces the same image layout.

FROM node:24-alpine AS frontend
WORKDIR /src
COPY package.json yarn.lock ./
RUN yarn install --frozen-lockfile
COPY . .
RUN yarn run build:production

FROM golang:1.27-alpine AS backend
ARG VERSION=develop
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor \
    -ldflags "-X github.com/palantir/policy-bot/version.version=${VERSION}" \
    -o /out/policy-bot .

FROM scratch
STOPSIGNAL SIGINT
COPY config/policy-bot.example.yml /secrets/policy-bot.yml
COPY docker/ca-certificates.crt /etc/ssl/certs/
COPY docker/mime.types /etc/
WORKDIR /policy-bot
COPY server/templates templates
COPY --from=frontend /src/build/static static
COPY --from=backend /out/policy-bot bin/linux-amd64/policy-bot
ENTRYPOINT ["bin/linux-amd64/policy-bot"]
CMD ["server", "--config", "/secrets/policy-bot.yml"]
