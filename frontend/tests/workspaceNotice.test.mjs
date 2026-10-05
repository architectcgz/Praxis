import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

// 先启动 Vite，用 PLAYWRIGHT_MODULE/BROWSER_PATH 指向已有 Playwright 和浏览器；BASE_URL 可指定服务地址。
const require = createRequire(import.meta.url)
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright')

test('/reload 使用不阻塞操作的轻提示，3 秒后自动关闭', async () => {
    const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_PATH })
    try {
        const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
        const errors = []
        page.on('pageerror', (error) => errors.push(error.message))
        await page.clock.install({ time: new Date('2026-01-01T00:00:00Z') })
        await page.clock.pauseAt(new Date('2026-01-01T00:00:01Z'))
        // 仅模拟桌面数据边界，命令、状态更新和提示渲染仍使用真实应用代码。
        await page.addInitScript(() => {
            const session = {
                id: 'session-test', projectId: 'project-test', workspaceId: 'workspace-test',
                title: '测试会话', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', agents: [],
            }
            window.go = { bindings: {
                ProjectBindings: { ListProjects: async () => [{ id: session.projectId, name: '测试项目', path: 'E:/test', defaultWorkspaceId: session.workspaceId }] },
                SessionBindings: { ListSessions: async () => [session], GetSession: async () => session },
                AgentBindings: {}, CommandBindings: {},
                ModelBindings: { ListModels: async () => [], ReloadConfig: async () => {
                    if (window.reloadFails) throw new Error('配置重新加载失败')
                } },
            } }
            window.runtime = { EventsOnMultiple: () => () => {} }
        })
        await page.goto(process.env.BASE_URL || 'http://127.0.0.1:5174')
        const input = page.getByRole('combobox', { name: '执行输入' })
        const toast = page.locator('.toast-notice')
        await input.waitFor({ state: 'visible' })
        const before = await page.locator('.main-panel').boundingBox()
        await input.fill('/reload')
        await input.press('Enter')
        await toast.waitFor({ state: 'visible' })
        assert.equal(await toast.textContent(), 'Praxis 配置已重新加载')
        assert.equal(await toast.getAttribute('role'), 'status')
        assert.equal(await toast.locator('button').count(), 0)
        assert.equal(await page.locator('.slash-command-notice, .modal-backdrop').count(), 0)
        assert.equal(await toast.evaluate((element) => element.parentElement === document.body), true)
        assert.equal(await toast.evaluate((element) => getComputedStyle(element).pointerEvents), 'none')
        assert.equal(await input.evaluate((element) => document.activeElement === element), true)
        assert.deepEqual(await page.locator('.main-panel').boundingBox(), before)
        await page.clock.runFor(2999)
        assert.equal(await toast.isVisible(), true)
        await page.clock.runFor(1)
        await toast.waitFor({ state: 'hidden' })

        await input.fill('/reload')
        await input.press('Enter')
        await toast.waitFor({ state: 'visible' })
        await page.clock.runFor(2000)
        await input.fill('/reload')
        await input.press('Enter')
        await toast.waitFor({ state: 'visible' })
        await page.clock.runFor(2000)
        assert.equal(await toast.isVisible(), true)

        const viewports = [
            [1440, 900], [1181, 800], [1180, 800], [861, 800], [860, 800],
            [721, 800], [720, 800], [461, 800], [460, 800], [320, 640], [860, 320],
        ]
        for (const [width, height] of viewports) {
            await page.setViewportSize({ width, height })
            const box = await toast.boundingBox()
            assert.ok(box && box.x >= 15 && box.x + box.width <= width - 15 && box.y + box.height <= height)
            assert.ok(Math.abs(box.x + box.width / 2 - width / 2) <= 1)
            assert.equal(await toast.evaluate((element) => element.scrollWidth <= element.clientWidth), true)
            if (width === 1440 || width === 320) {
                await page.screenshot({ path: join(tmpdir(), `praxis-reload-notice-${process.pid}-${width}.png`) })
            }
        }
        await page.emulateMedia({ reducedMotion: 'reduce' })
        await page.clock.runFor(1000)
        await toast.waitFor({ state: 'hidden' })

        await page.evaluate(() => { window.reloadFails = true })
        await input.fill('/reload')
        await input.press('Enter')
        await page.getByRole('alert').waitFor({ state: 'visible' })
        assert.equal(await toast.count(), 0)
        assert.equal(await input.inputValue(), '/reload')
        assert.deepEqual(errors, [])
    } finally {
        await browser.close()
    }
})
