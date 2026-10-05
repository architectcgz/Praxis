package dto

// FilePreview 是只读文本预览响应；Path 为工作区相对路径，Content 为完整 UTF-8 文本。
type FilePreview struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
