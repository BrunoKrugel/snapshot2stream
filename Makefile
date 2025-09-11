.PHONY: run
run:
	go run main.go

.PHONY: format
format:
	goimports -w .
	go fmt ./...
	fieldalignment -fix ./...
