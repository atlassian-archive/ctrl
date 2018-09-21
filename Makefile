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

.PHONY: generate-deepcopy
generate-deepcopy:
	go build -o build/bin/deepcopy-gen ./vendor/k8s.io/code-generator/cmd/deepcopy-gen
	build/bin/deepcopy-gen \
	--v 1 --logtostderr \
	--go-header-file "build/boilerplate.go.txt" \
	--input-dirs "github.com/atlassian/ctrl/apis/condition/v1" \
	--bounding-dirs "github.com/atlassian/ctrl/apis/condition/v1" \
	--output-file-base zz_generated.deepcopy
