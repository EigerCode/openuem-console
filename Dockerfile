FROM golang:1.23-bookworm AS build
COPY . ./

ENV GOTOOLCHAIN=auto

RUN go install github.com/a-h/templ/cmd/templ@v0.3.943
RUN templ generate
RUN CGO_ENABLED=1 go build -o "/bin/openuem-console" .

FROM debian:bookworm-slim
COPY --from=build /bin/openuem-console /bin/openuem-console
COPY ./assets /bin/assets
RUN apt-get update
RUN apt install -y ca-certificates
EXPOSE 1323
EXPOSE 1324
WORKDIR /bin
ENTRYPOINT ["/bin/openuem-console"]