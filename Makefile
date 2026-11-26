.PHONY: test generate

test:
	go test -v -count=1 ./...

generate:
	go run main.go
