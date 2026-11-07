.PHONY: build web server run test test-full fmt vet check clean

build: web server

web:
	tsc

server:
	go build -o tank-ops .

run: web
	go run .

test:
	go test -race -short ./...

# includes the slow bot-vs-bot strength comparison
test-full:
	go test ./...

fmt:
	gofmt -l -w .

vet:
	go vet ./...

check: vet test
	tsc --noEmit

clean:
	rm -f tank-ops
	find web/dist -name '*.js' -delete
