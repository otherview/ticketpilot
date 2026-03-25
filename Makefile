BIN := bin/ticketpilot

.PHONY: build clean test

build:
	cd ticketpilot-go && go build -o ../$(BIN) ./cmd/ticketpilot/

clean:
	rm -f $(BIN)

test:
	cd ticketpilot-go && go test ./...
