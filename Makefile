BINARY  := mdx
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/Quette420/go_rag/internal/cli.Version=$(VERSION)

.PHONY: build test vet fmt demo release clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/mdx

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

demo: build
	./$(BINARY) index -root examples/notes
	./$(BINARY) search -root examples/notes "AllocateChannelSequence"

release:
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64       ./cmd/mdx
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64      ./cmd/mdx
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe ./cmd/mdx

clean:
	rm -rf $(BINARY) $(BINARY).exe dist
