const rememberedModelKey = 'praxis.selected-model'

/** 用户显式选定的 Provider/Model 组合；跨 Agent 与 Session 复用。 */
export type RememberedModelSelection = {
    providerId: string
    modelId: string
    reasoning: string
}

const emptySelection: RememberedModelSelection = { providerId: '', modelId: '', reasoning: '' }

export function readRememberedModelSelection(): RememberedModelSelection {
    try {
        const raw = window.localStorage.getItem(rememberedModelKey)
        if (!raw) return emptySelection
        const parsed: unknown = JSON.parse(raw)
        if (!parsed || typeof parsed !== 'object') return emptySelection
        const value = parsed as Partial<RememberedModelSelection>
        return {
            providerId: typeof value.providerId === 'string' ? value.providerId : '',
            modelId: typeof value.modelId === 'string' ? value.modelId : '',
            reasoning: typeof value.reasoning === 'string' ? value.reasoning : '',
        }
    } catch {
        // 浏览器预览与 Wails webview 中本地存储都是可选的。
        return emptySelection
    }
}

/** 只记住完整的 Provider/Model 组合，避免把半截选择带到下一次启动。 */
export function rememberModelSelection(selection: RememberedModelSelection) {
    try {
        if (selection.providerId && selection.modelId) {
            window.localStorage.setItem(rememberedModelKey, JSON.stringify(selection))
        } else {
            window.localStorage.removeItem(rememberedModelKey)
        }
    } catch {
        // 同 readRememberedModelSelection：本地存储不可用时降级为会话内记忆。
    }
}
