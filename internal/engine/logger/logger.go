// Package logger deliberately discards upstream value-level logs, which may
// contain cookies, signed URLs or target metadata. Service logs are structured.
package logger

type Sink struct{}

var Logger = &Sink{}

func (*Sink) Errorf(string, ...interface{}) {}
func Debug(...interface{})                  {}
func Debugf(string, ...interface{})         {}
func Infof(string, ...interface{})          {}
func Warnf(string, ...interface{})          {}
func Errorf(string, ...interface{})         {}
