APP := myddns
VERSION := 2.0.0

.PHONY: build test fmt vet clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(APP) .

test:
	go test ./...

fmt:
	gofmt -w *.go

vet:
	go vet ./...

clean:
	rm -f $(APP)
