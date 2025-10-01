package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

var Logger *logrus.Logger

func init() {
	Logger = logrus.New()

	Logger.SetOutput(os.Stdout)
	Logger.SetLevel(logrus.InfoLevel)
	Logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
	})
}

// SetLevel sets the log level
func SetLevel(level logrus.Level) {
	Logger.SetLevel(level)
}

// SetDebugMode enables debug logging
func SetDebugMode() {
	Logger.SetLevel(logrus.DebugLevel)
	Logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})
}

// Info logs info level message
func Info(msg string, fields ...logrus.Fields) {
	if len(fields) > 0 {
		Logger.WithFields(fields[0]).Info(msg)
	} else {
		Logger.Info(msg)
	}
}

// Error logs error level message
func Error(msg string, err error, fields ...logrus.Fields) {
	entry := Logger.WithField("error", err)
	if len(fields) > 0 {
		entry = entry.WithFields(fields[0])
	}
	entry.Error(msg)
}

// Debug logs debug level message
func Debug(msg string, fields ...logrus.Fields) {
	if len(fields) > 0 {
		Logger.WithFields(fields[0]).Debug(msg)
	} else {
		Logger.Debug(msg)
	}
}

// Warn logs warning level message
func Warn(msg string, fields ...logrus.Fields) {
	if len(fields) > 0 {
		Logger.WithFields(fields[0]).Warn(msg)
	} else {
		Logger.Warn(msg)
	}
}
