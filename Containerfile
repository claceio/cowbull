############################
# STEP 1: Build the Go app #
############################

ARG GO_VERSION=1.24
ARG APP_NAME=cowbull

FROM golang:${GO_VERSION} AS builder

ARG APP_NAME

ENV CGO_ENABLED=0

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -ldflags="-s -w" -o ${APP_NAME} ./cmd/cowbull

##############################
# STEP 2: Create final image #
##############################

FROM alpine:latest

ARG APP_NAME
ENV APP_NAME=${APP_NAME}

WORKDIR /app

COPY --from=builder /app/${APP_NAME} .

RUN apk add --no-cache sqlite && \
    addgroup -S appgroup && adduser -S appuser -G appgroup && \
    mkdir /app/data && chown -R appuser:appgroup /app

USER appuser

# The sqlite database lives on a named volume so games survive rebuilds
VOLUME /app/data

ENTRYPOINT /app/${APP_NAME} -f /app/data/db.sqlite -l /app/data/GeoLite2-City.mmdb ${APP_ARGS}
