.PHONY: run
run:
	go run main.go

.PHONY: format
format:
	goimports -w .
	go fmt ./...
	fieldalignment -fix ./...

.PHONY: update
update:
	go get -t -u ./...

.PHONY: lint
lint:
	golangci-lint run
