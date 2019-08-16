ALL_GO_FILES=$$(find . -type f -name '*.go' -not -path "./vendor/*")
KUBE="kubernetes-1.15.1"
CLIENT="v12.0.0"

.PHONY: fmt
fmt:
	go build -o build/bin/goimports golang.org/x/tools/cmd/goimports
	build/bin/goimports -w=true -d $(ALL_GO_FILES)

.PHONY: test
test:
	go test -race ./...

# to update kubernetes version:
# 1. copy https://github.com/kubernetes/kubernetes/blob/v1.15.1/go.mod "require" section into go.mod
# 2. remove all v0.0.0 k8s.io/* statements
# 3. run this command
.PHONY: update-kube
update-kube:
	go get \
		k8s.io/api/core/v1@$(KUBE) \
		k8s.io/apimachinery@$(KUBE) \
		k8s.io/client-go@$(CLIENT) \
		k8s.io/code-generator/cmd/deepcopy-gen@$(KUBE) \
		github.com/stretchr/testify@v1.3.0
	go mod tidy

.PHONY: generate-deepcopy
generate-deepcopy:
	go build -o build/bin/deepcopy-gen k8s.io/code-generator/cmd/deepcopy-gen
	build/bin/deepcopy-gen \
	--v 1 --logtostderr \
	--go-header-file "build/boilerplate.go.txt" \
	--input-dirs "./apis/condition/v1" \
	--output-base "$(CURDIR)" \
	--bounding-dirs "./apis/condition/v1" \
	--output-file-base zz_generated.deepcopy
