/** 使用毫秒、秒、分、时展示耗时；不伪造缺失数据。 */
export function formatDuration(milliseconds: number): string {
    if (!Number.isFinite(milliseconds) || milliseconds < 0) return '未知'
    if (milliseconds < 1000) return `${Math.floor(milliseconds)} ms`
    const seconds = milliseconds / 1000
    if (seconds < 60) return `${(Math.floor(seconds * 10) / 10).toFixed(1)} s`
    if (seconds < 3600) return `${Math.floor(seconds / 60)} 分 ${Math.floor(seconds % 60)} 秒`
    return `${Math.floor(seconds / 3600)} 时 ${Math.floor(seconds % 3600 / 60)} 分 ${Math.floor(seconds % 60)} 秒`
}
