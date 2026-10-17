import { useEffect, useId, useState, type ReactNode } from 'react'
import { FileText, LoaderCircle, X } from 'lucide-react'
import { previewFile, type FilePreview as FilePreviewData } from '../../../api/filePreview'
import { Overlay } from '../../../components/ui'
import { readableError } from '../../../shared/errors'
import { FilePreviewContext, MessageMarkdown } from './MarkdownViews'
import './file-preview.css'

/** 为当前会话的所有输出提供文件预览；切换会话时由 key 卸载，避免展示其他会话的文件。 */
export function FilePreviewProvider({ sessionId, children }: { sessionId: string; children: ReactNode }) {
    const [path, setPath] = useState<string | null>(null)
    return <FilePreviewContext value={{ openFile: setPath }}>
        {children}
        {path !== null && <FilePreviewDialog key={path} sessionId={sessionId} path={path} onOpen={setPath} onClose={() => setPath(null)} />}
    </FilePreviewContext>
}

function FilePreviewDialog({ sessionId, path, onOpen, onClose }: {
    sessionId: string
    path: string
    onOpen: (path: string) => void
    onClose: () => void
}) {
    const titleId = useId()
    const [file, setFile] = useState<FilePreviewData | null>(null)
    const [error, setError] = useState('')
    useEffect(() => {
        let active = true
        void previewFile(sessionId, path).then(
            (value) => { if (active) setFile(value) },
            (reason) => { if (active) setError(readableError(reason)) },
        )
        return () => { active = false }
    }, [sessionId, path])
    return <Overlay labelledBy={titleId} onClose={onClose}>
        <section className="file-preview-dialog">
            <header className="file-preview-heading">
                <FileText size={18} aria-hidden="true" />
                <div><h2 id={titleId}>文件预览</h2><p>{file?.path || path}</p></div>
                <button className="icon-button" type="button" aria-label="关闭文件预览" title="关闭" onClick={onClose}><X size={18} aria-hidden="true" /></button>
            </header>
            <div className="file-preview-body scroll-region" aria-busy={!file && !error}>
                {error ? <p className="file-preview-error" role="alert">{error}</p> : !file ? (
                    <p className="file-preview-loading" role="status"><LoaderCircle size={16} className="is-spinning" aria-hidden="true" />正在读取文件…</p>
                ) : !file.content ? <p className="file-preview-empty">文件为空。</p> : /\.(md|markdown|mdown)$/i.test(file.path) ? (
                    <FilePreviewContext value={{ openFile: onOpen, basePath: file.path }}><MessageMarkdown content={file.content} /></FilePreviewContext>
                ) : <pre className="file-preview-text">{file.content}</pre>}
            </div>
        </section>
    </Overlay>
}
