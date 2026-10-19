FROM golang:1.27

WORKDIR /app

COPY go.mod go.sum ./
COPY app/ ./app/

RUN mkdir -p /data
RUN go build -o serveur-zone ./app

EXPOSE 8000

CMD ["./serveur-zone"]
