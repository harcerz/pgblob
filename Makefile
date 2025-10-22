.PHONY: build run test clean docker docker-run install

# Build the binary
build:
	CGO_ENABLED=1 go build -o pgserver .

# Run the server
run: build
	./pgserver

# Run tests
test:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run tests with coverage
coverage: test
	go tool cover -func=coverage.out

# Clean build artifacts
clean:
	rm -f pgserver
	rm -f coverage.out coverage.html
	rm -rf data/
	rm -f *.sqlite

# Build Docker image
docker:
	docker build -t pgserver:latest .

# Run with Docker Compose
docker-run:
	docker-compose up -d

# Stop Docker Compose
docker-stop:
	docker-compose down

# View Docker logs
docker-logs:
	docker-compose logs -f pgserver

# Install dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Install the binary
install:
	go install

# Run in development mode
dev:
	@echo "Running in development mode..."
	@export PG_PASSWORD=dev && \
	export LOG_LEVEL=debug && \
	export STORAGE=local && \
	./pgserver

# Create example config
config:
	cp config.yaml.example config.yaml
	cp .env.example .env

# Help
help:
	@echo "Available targets:"
	@echo "  build       - Build the binary"
	@echo "  run         - Build and run the server"
	@echo "  test        - Run tests with coverage"
	@echo "  coverage    - Show test coverage"
	@echo "  clean       - Remove build artifacts"
	@echo "  docker      - Build Docker image"
	@echo "  docker-run  - Run with Docker Compose"
	@echo "  docker-stop - Stop Docker Compose"
	@echo "  docker-logs - View Docker logs"
	@echo "  deps        - Download dependencies"
	@echo "  fmt         - Format code"
	@echo "  lint        - Lint code"
	@echo "  install     - Install binary"
	@echo "  dev         - Run in development mode"
	@echo "  config      - Create example config files"
	@echo "  help        - Show this help"
