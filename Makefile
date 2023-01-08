.PHONY: t
t: commit.txt
	go test .

.PHONY: commit.txt
commit.txt:
	@git rev-parse HEAD | tr -d "\n" > commit.txt
