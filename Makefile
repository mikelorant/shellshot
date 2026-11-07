export PKG_CONFIG_PATH := $(CURDIR)/third_party/libghostty/lib/pkgconfig
export CGO_ENABLED := 1

.PHONY: deps build test vet fmt clean

deps:
	./scripts/fetch-libghostty.sh

build:
	go build -o bin/shellshot ./cmd/shellshot

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin
