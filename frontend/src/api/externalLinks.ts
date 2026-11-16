import { Browser } from '@wailsio/runtime'
import { isBindingAvailable } from './bindings'

/** 网页链接交给系统浏览器；非桌面环境使用新标签页，始终不替换当前会话页面。 */
export async function openExternalURL(url: string): Promise<void> {
    if (!/^https?:\/\//i.test(url)) return
    if (isBindingAvailable()) await Browser.OpenURL(url)
    else window.open(url, '_blank', 'noopener,noreferrer')
}
