FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.work go.work.sum ./
RUN go mod download
COPY . .

ARG SERVICE=api-gateway
RUN go build -o /out/service ./apps/${SERVICE}

FROM alpine:3.22

RUN adduser -D -H auction
WORKDIR /app
COPY --from=build /out/service /usr/local/bin/service
COPY --from=build /src/migrations ./migrations
USER auction
ENTRYPOINT ["/usr/local/bin/service"]
