FROM golang:1.24.2

WORKDIR /app

COPY . .

RUN go mod tidy
RUN go build -o marine .

EXPOSE 50053

CMD ["./marine"]