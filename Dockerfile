FROM golang:1.25-alpine AS build

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

COPY cmd /app/cmd
COPY internal /app/internal

RUN go build -o /app/app /app/cmd/main.go

FROM golang:1.25-alpine

COPY --from=build /app/app /app/app

CMD [ "/app/app" ]
