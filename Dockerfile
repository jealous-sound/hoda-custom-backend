FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /hoda-server ./cmd/hoda-server

FROM alpine:3.20
COPY --from=build /hoda-server /usr/local/bin/hoda-server
ENV PORT=8787
EXPOSE 8787
CMD ["sh", "-c", "exec hoda-server -addr 0.0.0.0:${PORT} -data /data"]
