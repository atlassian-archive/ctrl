ALL_GO_FILES=$$(find . -type f -name '*.go' -not -path "./vendor/*")

.PHONY: setup
setup:
	go get -u golang.org/x/tools/cmd/goimports
	go get -u gopkg.in/alecthomas/gometalinter.v2
	gometalinter.v2 --install --force
	dep ensure

.PHONY: fmt
fmt:
	goimports -w=true -d $(ALL_GO_FILES)

.PHONY: lint
lint:
	gometalinter.v2 ./...

.PHONY: test
test:
	go test -race ./...
