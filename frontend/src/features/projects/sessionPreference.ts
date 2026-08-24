const rememberedSessionKey = 'praxis.selected-session'

export function readRememberedSession() {
    try {
        return window.localStorage.getItem(rememberedSessionKey) || ''
    } catch {
        return ''
    }
}

export function rememberSelectedSession(sessionID: string) {
    try {
        if (sessionID) {
            window.localStorage.setItem(rememberedSessionKey, sessionID)
        } else {
            window.localStorage.removeItem(rememberedSessionKey)
        }
    } catch {
        // Local storage is optional in the browser preview and Wails webview.
    }
}
