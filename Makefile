.PHONY: build clean

BIN := bin/aisb

build:
	go build -trimpath -ldflags='-s -w' -o $(BIN) .
	@ls -la $(BIN)

clean:
	rm -f $(BIN)
