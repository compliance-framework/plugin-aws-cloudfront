SHELL := /bin/bash

.PHONY: test clean build

test:
	@go test ./...

clean:
	@rm -rf dist/*

build: clean
	@mkdir -p dist/
	@go build -o dist/plugin main.go
