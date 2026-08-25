FROM golang:1.25.6-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/chores ./cmd/chores

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/chores /chores

EXPOSE 8080
ENTRYPOINT ["/chores"]
CMD ["-addr=:8080", "-db=/data/chores.db"]
