FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/terminal-resume ./cmd/terminal-resume

FROM alpine:3.22

RUN addgroup -S resume && adduser -S -G resume resume
WORKDIR /app
COPY --from=build /out/terminal-resume /app/terminal-resume
RUN mkdir -p /app/.data && chown -R resume:resume /app/.data

USER resume
EXPOSE 23234

ENTRYPOINT ["/app/terminal-resume"]
CMD ["--listen", "0.0.0.0:23234", "--host-key", "/app/.data/ssh_host_ed25519"]
