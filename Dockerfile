FROM golang:1.25.5 AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

COPY . ./

RUN go install github.com/a-h/templ/cmd/templ@v0.3.943
RUN templ generate
RUN CGO_ENABLED=1 go build -o "/bin/openuem-console" .

# --- Final Stage ---
FROM debian:bookworm-slim

COPY --from=build /bin/openuem-console /bin/openuem-console
COPY ./assets /bin/assets

RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*
    
EXPOSE 1323
EXPOSE 1324

WORKDIR /bin
ENTRYPOINT ["/bin/openuem-console"]
