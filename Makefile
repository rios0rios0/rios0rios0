.PHONY: test generate

test:
	go test -tags=unit -v -count=1 ./...

generate:
	go run main.go
