package logz

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"
)

func LogStructuredPanic() {
	if r := recover(); r != nil {
		logStructuredPanic(os.Stderr, r, time.Now().Format(time.RFC3339), debug.Stack())
		panic(r)
	}
}

func logStructuredPanic(out io.Writer, i interface{}, time string, stack []byte) {
	bytes, err := json.Marshal(struct {
		Level   string `json:"level"`
		Time    string `json:"time"`
		Message string `json:"msg"`
		Stack   string `json:"stack"`
	}{
		Level:   "fatal",
		Time:    time,
		Message: fmt.Sprintf("%v", i),
		Stack:   string(stack),
	})
	if err != nil {
		fmt.Fprintf(out, "error while serializing panic: %+v\n", err) // nolint: errcheck, gas
		fmt.Fprintf(out, "original panic: %+v\n", i)                  // nolint: errcheck, gas
		return
	}
	fmt.Fprintf(out, "%s\n", bytes) // nolint: errcheck, gas
}
