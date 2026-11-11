package hook

import (
	"context"
	appcontext "praxis/internal/core/context"
)

// ContextBuilt 只读观察已构建的模型上下文，不允许修改共享 Entry 或返回替代正文。
type ContextBuilt struct {
	Identity
	Context appcontext.ModelContext
}

type AfterContextBuildHook interface {
	AfterContextBuild(context.Context, ContextBuilt) error
}

// ContextCompression 表示一次原生压缩策略调用；是否需要压缩由策略决定。
type ContextCompression struct {
	Identity
	Context appcontext.ModelContext
}

type ContextCompressionResult struct {
	ContextCompression
	ContextAfter appcontext.ModelContext
	Changed      bool
	Err          error
}

type BeforeContextCompressionHook interface {
	BeforeContextCompression(context.Context, ContextCompression) error
}

type AfterContextCompressionHook interface {
	AfterContextCompression(context.Context, ContextCompressionResult) error
}
