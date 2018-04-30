ALL_GO_FILES=$$(find . -type f -name '*.go' -not -path "./vendor/*")

.PHONY: setup
setup:
	go get -u golang.org/x/tools/cmd/goimports
	dep ensure

.PHONY: fmt
fmt:
	goimports -w=true -d $(ALL_GO_FILES)
