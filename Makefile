.PHONY: build web server run test fmt vet check clean

build: web server

web:
	tsc

server:
	go build -o tank-ops .

run: web
	go run .

test:
	go test -race ./...

fmt:
	gofmt -l -w .

vet:
	go vet ./...

check: vet test
	tsc --noEmit

clean:
	rm -f tank-ops
	find web/dist -name '*.js' -delete
