FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-trimpath go build -ldflags="-s -w" -o /out/ ./cmd/observer ./cmd/decider ./cmd/verifier ./cmd/checker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /
USER 65532:65532
