FROM golang:1.25 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/engram ./cmd/engram

FROM gcr.io/distroless/static-debian12

ENV ENGRAM_DATA_DIR=/data
ENV ENGRAM_HOST=0.0.0.0
ENV ENGRAM_PORT=7437

WORKDIR /
COPY --from=builder /out/engram /engram

EXPOSE 7437
VOLUME ["/data"]

ENTRYPOINT ["/engram"]
CMD ["serve"]
