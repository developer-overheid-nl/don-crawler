FROM golang:1.26-trixie AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/main main.go

# The crawler shells out to git (and git to libcurl for https), so the runtime
# image only needs git and CA certificates. Upgrading on every build pulls in
# Debian security fixes that the base image has not picked up yet.
FROM debian:trixie-slim

RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends ca-certificates git && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=build /out/main ./main
COPY vitality-ranges.yml ./

ENV GIN_MODE=release

EXPOSE 1337

CMD ["./main", "crawl"]
