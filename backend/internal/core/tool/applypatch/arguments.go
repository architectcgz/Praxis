package applypatch

import "fmt"

// Arguments 保存补丁及根目录，路径由调用编排统一解析。
type Arguments struct {
	Patch string `json:"patch"`
	Path  string `json:"path"`
}

// NewArguments 提供业务默认补丁根目录，由编排层在此基础上解码显式输入。
func NewArguments() Arguments {
	return Arguments{Path: "."}
}

func (a *Arguments) PathArgument() *string { return &a.Path }

// Prepare 接收编排层已解析的根目录，只校验补丁格式，不读写目标文件。
func Prepare(args Arguments) (Arguments, error) {
	invalidArguments := func(reason string) (Arguments, error) {
		return Arguments{}, fmt.Errorf("apply_patch arguments are invalid: %s", reason)
	}
	if args.Patch == "" {
		return invalidArguments("patch is required")
	}
	if len(args.Patch) > maxPatchBytes {
		return invalidArguments("patch is too large")
	}
	if _, err := Parse(args.Patch); err != nil {
		return invalidArguments(err.Error())
	}
	return args, nil
}
