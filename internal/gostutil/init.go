// Package gostutil supplies the global defaults expected by embedded GOST code.
package gostutil

import (
	"github.com/go-gost/core/logger"
	xlogger "github.com/go-gost/x/logger"
)

func init() {
	if logger.Default() == nil {
		logger.SetDefault(xlogger.Nop())
	}
}
