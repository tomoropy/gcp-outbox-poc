FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/simulator ./cmd/simulator
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/expire_billing ./cmd/jobs/expire_billing
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/outbox_cleanup ./cmd/jobs/outbox_cleanup

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/* /app/
CMD ["/app/api"]
