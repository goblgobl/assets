.PHONY: t
t: commit.txt
	go test -race -count=1 .

.PHONY: commit.txt
commit.txt:
	@git rev-parse HEAD | tr -d "\n" > commit.txt

.PHONY: build
build: commit.txt
	go build -ldflags="-s -w" -o assets cmd/main.go
