export PATH := $(HOME)/go/bin:$(PATH)

BINARY  := blackeye
GOFLAGS := -trimpath
GO      := $(HOME)/go/bin/go

.PHONY: all clean re run test tidy vendor lint

# Build the binary
all: vendor
	$(GO) build $(GOFLAGS) -o $(BINARY) .

# Clean build artifacts
clean:
	rm -f $(BINARY) coverage.out

# Rebuild from scratch
re: clean all

# Build and run
run: all
	./$(BINARY)

# Run tests
test: vendor
	$(GO) test ./internal/... -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -func=coverage.out | grep total

# Resolve and tidy modules
tidy:
	$(GO) mod tidy

# Vendor all dependencies
vendor: tidy
	$(GO) mod vendor

# Basic lint check
lint:
	$(GO) vet ./...
