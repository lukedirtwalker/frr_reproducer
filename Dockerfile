FROM golang:1.23.6-alpine3.21 as builder

COPY . .
RUN go build -o /frr_test

FROM alpine:3.21

COPY --from=builder /frr_test /frr_test
CMD ["/frr_test"]
