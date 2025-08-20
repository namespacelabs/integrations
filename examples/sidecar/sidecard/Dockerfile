FROM golang AS builder

# simple and not efficient.
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o sidecard .

FROM scratch

COPY --from=builder /src/sidecard /entrypoint

