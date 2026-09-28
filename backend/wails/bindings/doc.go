// Package bindings 是 Wails 适配层：定义 binding 契约（req/resp DTO 与错误码），
// 把这些契约映射到本包 services.go 定义的窄接口集，并通过 Runtime 获取桌面 context。
package bindings
