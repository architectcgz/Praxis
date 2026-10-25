package bash

import (
	"fmt"
	"math"
	"strings"
)

const (
	maxCommandBytes = 128 * 1024
	defaultTimeout  = 120
	maxTimeout      = 600
)

// Arguments 在解码、参数准备和执行阶段使用同一类型。
type Arguments struct {
	Command string  `json:"command"`
	Path    string  `json:"path"`
	Timeout float64 `json:"timeout,omitzero"`
}

// NewArguments 提供业务默认目录和超时；显式超时为零不会回退到默认值。
func NewArguments() Arguments {
	return Arguments{
		Path:    ".",
		Timeout: defaultTimeout,
	}
}

func (a *Arguments) PathArgument() *string { return &a.Path }

// Prepare 接收编排层已解析的工作目录，只校验命令和超时范围。
func Prepare(args Arguments) (Arguments, error) {
	invalidArguments := func(reason string) (Arguments, error) {
		return Arguments{}, fmt.Errorf("bash arguments are invalid: %s", reason)
	}
	if strings.TrimSpace(args.Command) == "" {
		return invalidArguments("command is required")
	}
	if len(args.Command) > maxCommandBytes {
		return invalidArguments("command is too long")
	}
	if math.IsNaN(args.Timeout) || math.IsInf(args.Timeout, 0) ||
		args.Timeout <= 0 || args.Timeout > maxTimeout {
		return invalidArguments("timeout must be greater than 0 and within the supported range")
	}
	return args, nil
}
