.PHONY: build run demo clean

build:
	go build -o bin/hotreload ./cmd/hotreload

run: build
	./bin/hotreload --root . --build "go build -o bin/server ./cmd/hotreload" --exec "./bin/server"

demo: build
	./bin/hotreload --root ./testserver --build "go build -o ./bin/server ." --exec "./bin/server"

clean:
	rm -rf bin/ ./testserver/bin/
