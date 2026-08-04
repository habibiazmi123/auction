FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.work go.work.sum ./
RUN go mod download
COPY . .

ARG SERVICE_PATH=./cmd/service
RUN go build -o /out/service "${SERVICE_PATH}"

FROM alpine:3.22

RUN adduser -D -H auction
COPY --from=build /out/service /usr/local/bin/service
USER auction
ENTRYPOINT ["/usr/local/bin/service"]
