PLUGIN_NAME := rb-manager
GO_BUILD    := go build -o $(PLUGIN_NAME)

.PHONY: setup build install clean test lint

setup:
	go mod tidy

build: setup
	$(GO_BUILD) .

install: build
	mkdir -p $(HOME)/.jfrog/plugins/$(PLUGIN_NAME)/bin
	cp $(PLUGIN_NAME) $(HOME)/.jfrog/plugins/$(PLUGIN_NAME)/bin/

clean:
	rm -f $(PLUGIN_NAME)

test: setup
	go test ./...

lint: setup
	go vet ./...
