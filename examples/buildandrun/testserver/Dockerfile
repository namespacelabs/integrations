FROM golang AS builder

WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o server .

FROM scratch

COPY --from=builder /src/server /server

ENTRYPOINT [ "./server" ]