GO ?= go
BINARY ?= n8n-turbo

.PHONY: build run docker-build docker-up clean

build:
	$(GO) build -trimpath -o $(BINARY) ./cmd/n8n-turbo

run: build
	./$(BINARY)

docker-build:
	docker build -t n8n-turbo:latest .

docker-up:
	docker compose up -d --build

clean:
	rm -f $(BINARY) $(BINARY).exe
