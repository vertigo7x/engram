FROM golang:1.25 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/postgram ./cmd/postgram

FROM gcr.io/distroless/static-debian12

ENV POSTGRAM_DATA_DIR=/data
ENV POSTGRAM_HOST=0.0.0.0
ENV POSTGRAM_PORT=7437

WORKDIR /
COPY --from=builder /out/postgram /postgram

EXPOSE 7437
VOLUME ["/data"]

ENTRYPOINT ["/postgram"]
CMD ["serve"]
