package logz

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func Controller(obj meta_v1.Object) zapcore.Field {
	return ControllerName(obj.GetName())
}

func ControllerName(name string) zapcore.Field {
	return zap.String("ctrl_name", name)
}

func ControllerGk(gk schema.GroupKind) zapcore.Field {
	return zap.Stringer("ctrl_gk", &gk)
}

func Object(obj meta_v1.Object) zapcore.Field {
	return ObjectName(obj.GetName())
}

func ObjectName(name string) zapcore.Field {
	return zap.String("obj_name", name)
}

func ObjectGk(gk schema.GroupKind) zapcore.Field {
	return zap.Stringer("obj_gk", &gk)
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

func IterationName(iteration uint32) zapcore.Field {
	return zap.Uint32("iter", iteration)
}

func Logger(loggingLevel, logEncoding string) *zap.Logger {
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
	cfg := zap.NewProductionEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	lockedSyncer := zapcore.Lock(zapcore.AddSync(os.Stderr))
	return zap.New(
		zapcore.NewCore(
			logEncoder(cfg),
			lockedSyncer,
			levelEnabler,
		),
		zap.ErrorOutput(lockedSyncer),
	)
}
