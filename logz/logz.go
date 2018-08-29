package logz

import (
	"os"

	"github.com/atlassian/ctrl"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ControllerGk is a zap field used to identify logs coming from a specific controller
// or controller constructor. It includes logs that don't involve processing an
// object.
func ControllerGk(gk schema.GroupKind) zapcore.Field {
	return zap.Stringer("ctrl_gk", &gk)
}

// Object returns a zap field used to record ObjectName.
func Object(obj meta_v1.Object) zapcore.Field {
	return ObjectName(obj.GetName())
}

// ObjectName is a zap field to identify logs with the object name of a specific
// object being processed in the ResourceEventHandler or in the Controller.
func ObjectName(name string) zapcore.Field {
	return zap.String("obj_name", name)
}

// ObjectGk is a zap field to identify logs with the object gk of a specific
// object being processed in the ResourceEventHandler or in the Controller.
func ObjectGk(gk schema.GroupKind) zapcore.Field {
	return zap.Stringer("obj_gk", &gk)
}

// Operation is a zap field used in ResourceEventHandler to identify the operation
// that the logs are being produced from.
func Operation(operation ctrl.Operation) zapcore.Field {
	return zap.Stringer("operation", operation)
}

func Namespace(obj meta_v1.Object) zapcore.Field {
	return NamespaceName(obj.GetNamespace())
}

func NamespaceName(namespace string) zapcore.Field {
	if namespace == "" {
		return zap.Skip()
	}
	return zap.String("namespace", namespace)
}

func Iteration(iteration uint32) zapcore.Field {
	return zap.Uint32("iter", iteration)
}

func Logger(level zapcore.Level, encoder func(zapcore.EncoderConfig) zapcore.Encoder) *zap.Logger {
	cfg := zap.NewProductionEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.TimeKey = "time"
	lockedSyncer := zapcore.Lock(zapcore.AddSync(os.Stderr))
	return zap.New(
		zapcore.NewCore(
			encoder(cfg),
			lockedSyncer,
			level,
		),
		zap.ErrorOutput(lockedSyncer),
	)
}

func LoggerStr(loggingLevel, logEncoding string) *zap.Logger {
	var levelEnabler zapcore.Level
	switch loggingLevel {
	case "debug":
		levelEnabler = zap.DebugLevel
	case "warn":
		levelEnabler = zap.WarnLevel
	case "error":
		levelEnabler = zap.ErrorLevel
	default:
		levelEnabler = zap.InfoLevel
	}
	var logEncoder func(zapcore.EncoderConfig) zapcore.Encoder
	if logEncoding == "console" {
		logEncoder = zapcore.NewConsoleEncoder
	} else {
		logEncoder = zapcore.NewJSONEncoder
	}
	return Logger(levelEnabler, logEncoder)
}
