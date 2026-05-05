.PHONY: build clean

BIN := bin/psb

build:
	go build -trimpath -ldflags='-s -w' -o $(BIN) .
	@ls -la $(BIN)

clean:
	rm -f $(BIN)
