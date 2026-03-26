.PHONY: build install clean test

build:
	go build -o chrome-pilot .

install:
	go install .

clean:
	rm -f chrome-pilot

test:
	go test ./... -v
