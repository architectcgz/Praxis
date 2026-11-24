package tools

import (
	"bytes"
	"errors"
	"os"
)

// outputWriter 在达到内联上限前保留小缓冲，超过上限后将已有和后续输出完整写入文件。
// 同一个 outputWriter 同时交给 exec.Cmd 的 stdout/stderr，由 exec 保证串行写入。
type outputWriter struct {
	directory   string
	inlineBytes int
	buffer      bytes.Buffer
	file        *os.File
	size        int64
	err         error
}

func newOutputWriter(directory string, inlineBytes int) *outputWriter {
	return &outputWriter{
		directory:   directory,
		inlineBytes: inlineBytes,
	}
}

func (w *outputWriter) Write(value []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.file == nil && w.buffer.Len()+len(value) <= w.inlineBytes {
		return w.buffer.Write(value)
	}
	if w.file == nil {
		w.file, w.err = createOutputFile(w.directory)
		if w.err != nil {
			return 0, w.err
		}
		var size int
		size, w.err = w.file.Write(w.buffer.Bytes())
		w.size = int64(size)
		w.buffer.Reset()
		if w.err != nil {
			return 0, w.err
		}
	}
	size, err := w.file.Write(value)
	w.size += int64(size)
	w.err = err
	return size, err
}

// Finish 关闭文件后才发布路径；保存失败的半成品文件不会被写入工具回执。
func (w *outputWriter) Finish() (string, outputFile, error) {
	if w.file == nil {
		return w.buffer.String(), outputFile{}, w.err
	}
	err := errors.Join(w.err, w.file.Close())
	if err != nil {
		_ = os.Remove(w.file.Name())
		return "", outputFile{}, err
	}
	return "", outputFile{Path: w.file.Name(), Size: w.size}, nil
}
