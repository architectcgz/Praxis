// Package document 负责以内容寻址方式保存不可变 JSON 文档。
package document

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store 是基于内容寻址的文档存储：文档名由内容哈希决定，同一内容天然幂等复用。
//
// 业务约束：必须先发布文档、再提交日志中的文档引用（见 Put）。这样即使事务失败，
// 也只会残留一份无人引用的文档，绝不会出现日志引用指向缺失数据。
type Store struct {
	root string
	mu   sync.RWMutex
}

// New 创建落盘到文件系统的文档存储，root 为存储根目录。
// 要求 root 为绝对路径且不可为文件系统根，避免文档被写到危险位置。
func New(root string) (*Store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		return nil, errors.New("document root must be an absolute non-root path")
	}
	return &Store{root: filepath.Clean(root)}, nil
}

// Put 校验并原子发布一份不可变 JSON 文档，返回可写入日志记录的引用。
//
// collection 与 id 代表业务维度（聚合类型与聚合 ID，如 task/<id>），
// 二者共同决定目录；文件名由内容哈希决定，因此重复写入同一内容会直接复用。
// 写入前会拒绝含敏感字段或非法路径片段的文档。
func (s *Store) Put(ctx context.Context, collection, id string, value any) (string, error) {
	if ctx == nil {
		return "", errors.New("document write context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !isValidDocumentKeyPart(collection) || !isValidDocumentKeyPart(id) {
		return "", errors.New("document collection and id are invalid")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode document: %w", err)
	}
	var checked any
	if err := json.Unmarshal(encoded, &checked); err != nil {
		return "", fmt.Errorf("inspect document: %w", err)
	}
	if containsSensitiveValue(checked) {
		return "", errors.New("document contains a sensitive field")
	}
	digest := sha256.Sum256(encoded)
	name := "sha256-" + hex.EncodeToString(digest[:])
	ref := filepath.ToSlash(filepath.Join(collection, id, name+".json"))

	s.mu.Lock()
	defer s.mu.Unlock()
	destination := filepath.Join(s.root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", fmt.Errorf("create document directory: %w", err)
	}
	if _, err := os.Stat(destination); err == nil {
		return ref, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect document: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".document-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create document temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("protect document temporary file: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write document: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync document: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close document temporary file: %w", err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return "", fmt.Errorf("publish document: %w", err)
	}
	return ref, nil
}

// Get 按日志记录中的引用读取文档并反序列化到 target。
// 引用格式非法或文档不存在时返回错误（不存在时返回 os.ErrNotExist）。
func (s *Store) Get(ctx context.Context, ref string, target any) error {
	if ctx == nil {
		return errors.New("document read context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if target == nil || !isValidDocumentRef(ref) {
		return errors.New("document reference is invalid")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	encoded, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(ref)))
	if errors.Is(err, os.ErrNotExist) {
		return os.ErrNotExist
	}
	if err != nil {
		return fmt.Errorf("read document: %w", err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("decode document: %w", err)
	}
	return nil
}

// Remove 删除一份不再被业务日志引用的文档。
func (s *Store) Remove(ctx context.Context, ref string) error {
	if ctx == nil {
		return errors.New("document removal context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !isValidDocumentRef(ref) {
		return errors.New("document reference is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(filepath.Join(s.root, filepath.FromSlash(ref))); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("remove document: %w", err)
	}
	return nil
}

// isValidDocumentKeyPart 判断 value 能否作为文档 key 的单层目录片段使用：
// collection（聚合类型）与 id（聚合 ID）都必须是安全片段，不能逃逸出存储根目录。
func isValidDocumentKeyPart(value string) bool {
	return value != "" && value != "." && value != ".." &&
		filepath.Base(value) == value && !strings.ContainsAny(value, "/\\:\x00\r\n")
}

// isValidDocumentRef 判断 value 是否为格式正确的内容寻址引用，
// 即 日志记录中保存的 <collection>/<id>/sha256-<hex>.json。
func isValidDocumentRef(value string) bool {
	value = filepath.ToSlash(strings.TrimSpace(value))
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	parts := strings.Split(value, "/")
	return len(parts) == 3 && isValidDocumentKeyPart(parts[0]) && isValidDocumentKeyPart(parts[1]) &&
		strings.HasPrefix(parts[2], "sha256-") && strings.HasSuffix(parts[2], ".json")
}

func containsSensitiveValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "apikey", "api_key", "authorization", "secret", "token", "providerpayload", "provider_payload", "hiddenprompt", "hidden_prompt":
				return true
			}
			if containsSensitiveValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveValue(child) {
				return true
			}
		}
	}
	return false
}
