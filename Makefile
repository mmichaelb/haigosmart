# Constitution gates, one command each.
GO ?= go

.PHONY: fmt vet test bench build run check
fmt:
	$(GO) fmt ./...
vet:
	$(GO) vet ./...
test:
	$(GO) test ./... -race
bench:
	$(GO) test -bench=. -benchmem ./...
build:
	$(GO) build -o haigosmartd ./cmd/haigosmartd
run: build
	./haigosmartd
check: fmt vet test
