.PHONY: build run demo clean

build:
	go build -o bin/autoreload ./cmd/autoreload

run: build
	./bin/autoreload --root . --build "go build -o bin/server ./cmd/autoreload" --exec "./bin/server"

demo: build
	./bin/autoreload --root ./testserver --build "go build -o ./bin/server ." --exec "./bin/server"

clean:
	rm -rf bin/ ./testserver/bin/
