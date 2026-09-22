FROM golang:1.24-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/mc-map-uploader ./cmd/mc-map-uploader

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/mc-map-uploader /mc-map-uploader
WORKDIR /app
USER 65532:65532
ENTRYPOINT ["/mc-map-uploader"]
CMD ["run", "/app/config.yaml"]
