# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/eetmeter-sync ./cmd/eetmeter-sync

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/eetmeter-sync /eetmeter-sync
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/eetmeter-sync"]
