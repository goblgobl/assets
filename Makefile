.PHONY: t
t: commit.txt
	go test -race -count=1 .

.PHONY: commit.txt
commit.txt:
	@git rev-parse HEAD | tr -d "\n" > commit.txt
