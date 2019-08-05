// +build tools

package ctrl

// https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module

import (
	_ "k8s.io/code-generator/cmd/deepcopy-gen"
)
