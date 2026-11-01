package hook

import (
	"context"
	appcontext "praxis/internal/core/context"
)

// ContextBuild 在纯 ContextBuilder 构建前提供本次请求副本。
// 变换结果不回写历史或 Run Context；每轮都从基础上下文重新变换。
type ContextBuild struct {
	Identity
	Content appcontext.Content
	Limits  appcontext.Limits
}

func cloneContent(value appcontext.Content) appcontext.Content {
	copy := (appcontext.ModelContext{SystemPrompt: value.SystemPrompt, Entries: value.Entries}).Clone()
	return appcontext.Content{SystemPrompt: copy.SystemPrompt, Entries: copy.Entries}
}

func (e ContextBuild) clone() ContextBuild {
	e.Content = cloneContent(e.Content)
	return e
}

type ContextBuilt struct {
	Identity
	Context appcontext.ModelContext
	Err     error
}

func (e ContextBuilt) clone() ContextBuilt {
	e.Context = e.Context.Clone()
	return e
}

type BeforeContextBuildHook interface {
	BeforeContextBuild(context.Context, ContextBuild) (appcontext.Content, error)
}

type AfterContextBuildHook interface {
	AfterContextBuild(context.Context, ContextBuilt) error
}

// ContextCompression 表示一次原生压缩策略调用；是否需要压缩由策略决定。
type ContextCompression struct {
	Identity
	Context appcontext.ModelContext
}

func (e ContextCompression) clone() ContextCompression {
	e.Context = e.Context.Clone()
	return e
}

type ContextCompressionResult struct {
	ContextCompression
	ContextAfter appcontext.ModelContext
	Changed      bool
	Err          error
}

func (e ContextCompressionResult) clone() ContextCompressionResult {
	e.ContextCompression = e.ContextCompression.clone()
	e.ContextAfter = e.ContextAfter.Clone()
	return e
}

type BeforeContextCompressionHook interface {
	BeforeContextCompression(context.Context, ContextCompression) error
}

type AfterContextCompressionHook interface {
	AfterContextCompression(context.Context, ContextCompressionResult) error
}
