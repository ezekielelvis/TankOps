.PHONY: build web server run test test-full fmt vet check clean

build: web server

web:
	tsc

server:
	go build -o tank-ops ./cmd/tank-ops

run: web
	go run ./cmd/tank-ops

test:
	go test -race -short ./...

# includes the slow bot-vs-bot strength comparison
test-full:
	go test ./...

fmt:
	gofmt -l -w .

vet:
	# -composites=false: coordinate literals like Vector{1, 0} are deliberate
	go vet -composites=false ./...

check: vet test
	tsc --noEmit

clean:
	rm -f tank-ops
	find web/dist -name '*.js' -delete
