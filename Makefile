.PHONY: all build-windows-amd64 build-windows-arm64 build-local test vet

all: build-windows-amd64 build-windows-arm64

build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o dist/preflight-windows-amd64.exe .

build-windows-arm64:
	GOOS=windows GOARCH=arm64 go build -o dist/preflight-windows-arm64.exe .

build-local:
	go build -o dist/preflight .

test:
	go test ./...

vet:
	go vet ./...
