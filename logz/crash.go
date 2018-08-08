package logz

import (
	"fmt"
	"runtime/debug"

	"go.uber.org/zap"
)

func LogStructuredPanic(logger *zap.Logger) {
	if r := recover(); r != nil {
		// calling Error() instead of Fatal() or Panic() because those invoke os.Exit and panic()
		logger.Error(fmt.Sprintf("%v", r),
			zap.Any("panic", r),
			zap.String("stack", string(debug.Stack())),
		)
		panic(r)
	}
}
