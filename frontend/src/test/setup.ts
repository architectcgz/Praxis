import { afterEach, beforeEach } from 'vitest'
import { cleanup } from '@testing-library/react'

beforeEach(() => {
    const host = document.createElement('div')
    host.id = 'overlay-root'
    document.body.append(host)
})
afterEach(() => { cleanup(); document.getElementById('overlay-root')?.remove() })

Object.defineProperty(window, 'matchMedia', { writable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) })
Object.defineProperty(HTMLElement.prototype, 'scrollTo', { value: function (this: HTMLElement, options?: ScrollToOptions | number) {
    this.scrollTop = typeof options === 'number' ? options : options?.top || 0
} })
