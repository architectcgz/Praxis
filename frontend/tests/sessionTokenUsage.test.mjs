import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

// 复用已有 Playwright 与浏览器，运行时通过环境变量指定，不新增项目依赖。
const require = createRequire(import.meta.url)
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright')

test('会话及逐次请求 token、实时去重、恢复与响应式布局', async () => {
    const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_PATH })
    try {
        const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
        const errors = []
        page.on('pageerror', (error) => errors.push(error.message))
        await page.addInitScript(() => {
            const at = '2026-01-01T00:00:00Z'
            const agent = {
                id: 'primary', name: '主代理', sessionId: 'session', definitionId: 'primary', securityPolicyRevision: 1,
                profile: 'primary', state: 'idle', currentTurnId: '', turnIds: [], turns: [],
            }
            const session = { id: 'session', title: 'Token 统计测试', projectId: 'project', workspaceId: 'workspace', createdAt: at, updatedAt: at, agents: [agent] }
            const callbacks = new Set()
            window.testAgent = agent
            window.agentHistory = [1, 2].map((step) => ({
                kind: 'message', sequence: step, at,
                message: { id: `assistant:turn:${step}`, sequence: step, at, turnId: 'turn', role: 'assistant', content: `第 ${step} 次模型请求输出` },
            }))
            window.timings = [1, 2].map((step) => ({
                id: `timing-${step}`, sessionId: 'session', agentId: 'primary', turnId: 'turn', kind: 'provider', name: 'provider/model',
                referenceId: `assistant:turn:${step}`, startedAt: at, finishedAt: '2026-01-01T00:00:14.200Z',
                durationMs: 14200, firstResponseMs: 4800, status: 'completed', revision: 2,
            }))
            window.usageRecords = [{ sessionId: 'session', agentId: 'primary', turnId: 'turn', step: 1, usage: { inputTokens: 120, outputTokens: 30, cacheReadInputTokens: 80 } }]
            window.usageLoadFails = false
            window.emitAgentEvent = (event) => { for (const callback of callbacks) callback(event) }
            window.go = { bindings: {
                ProjectBindings: { ListProjects: async () => [{ id: 'project', name: '测试项目', path: 'E:/test', defaultWorkspaceId: 'workspace', state: 'active' }] },
                SessionBindings: { ListSessions: async () => [session], GetSession: async () => session },
                AgentBindings: {
                    GetAgent: async () => agent, ListAgentHistory: async () => window.agentHistory, ListAgentTimings: async () => window.timings,
                    ListSessionUsage: async () => {
                        if (window.usageLoadFails) throw new Error('用量读取失败')
                        if (window.holdUsageQuery) return new Promise((resolve) => { window.resolveUsageQuery = resolve })
                        return window.usageRecords
                    },
                },
                CommandBindings: {},
                ModelBindings: { ListModels: async () => [{
                    providerId: 'provider', providerName: 'Provider', modelId: 'model', label: '测试模型',
                    defaultProviderId: 'provider', defaultModelId: 'model', reasoningLevels: ['low', 'medium', 'high'],
                    defaultReasoningLevel: 'medium', assignedAgentDefinitions: ['primary'],
                }] },
            } }
            window.runtime = { EventsOnMultiple: (_, callback) => {
                callbacks.add(callback)
                return () => callbacks.delete(callback)
            } }
        })
        await page.goto(process.env.BASE_URL || 'http://127.0.0.1:5173')
        const usage = page.locator('.composer-token-usage')
        const input = page.getByRole('combobox', { name: '执行输入' })
        await usage.waitFor({ state: 'visible' })
        await page.waitForFunction(() => document.querySelector('.composer-token-usage')?.textContent === '150 tokens')
        assert.match(await usage.getAttribute('title'), /输入：120 tokens/)
        assert.match(await usage.getAttribute('title'), /输出：30 tokens/)
        assert.match(await usage.getAttribute('title'), /缓存读取：80 tokens/)
        const requests = page.locator('.message-copy-actions .operation-token-usage')
        assert.equal(await requests.count(), 2)
        assert.equal(await requests.nth(0).textContent(), '· 消耗 150 tokens')
        assert.equal(await requests.nth(1).textContent(), '· 消耗 -- tokens')
        assert.match(await requests.nth(0).getAttribute('title'), /输入：120 tokens\n输出：30 tokens/)
        assert.match(await page.locator('.message-copy-actions .message-timing').first().textContent(), /耗时 14\.2 s · 首响应 4\.8 s.*· 消耗 150 tokens/)

        await page.evaluate(() => {
            const event = { kind: 'model_usage', sessionId: 'session', agentId: 'primary', turnId: 'turn', step: 1, usage: { inputTokens: 120, outputTokens: 50, cacheReadInputTokens: 80 } }
            window.emitAgentEvent(event)
            window.emitAgentEvent(event)
            window.emitAgentEvent({ ...event, sessionId: 'another-session', turnId: 'another-turn' })
        })
        await page.waitForFunction(() => document.querySelector('.composer-token-usage')?.textContent === '170 tokens')
        assert.equal(await requests.nth(0).textContent(), '· 消耗 170 tokens')
        assert.equal(await requests.nth(1).textContent(), '· 消耗 -- tokens')
        await page.evaluate(() => {
            window.emitAgentEvent({ kind: 'model_usage', sessionId: 'session', agentId: 'delegate', turnId: 'delegate-turn', step: 1, usage: { inputTokens: 50, outputTokens: 10 } })
            window.emitAgentEvent({ kind: 'settled', sessionId: 'session', agentId: 'primary', turnId: 'turn' })
        })
        await page.waitForFunction(() => document.querySelector('.composer-token-usage')?.textContent === '230 tokens')
        assert.equal(await requests.nth(0).textContent(), '· 消耗 170 tokens')
        await page.evaluate(() => window.emitAgentEvent({ kind: 'model_usage', sessionId: 'session', agentId: 'primary', turnId: 'turn', step: 2, usage: { inputTokens: 10000, outputTokens: 234 } }))
        await page.waitForFunction(() => document.querySelectorAll('.message-copy-actions .operation-token-usage')[1]?.textContent === '· 消耗 10,234 tokens')

        const viewports = [[1440, 900], [1181, 800], [1180, 800], [861, 800], [860, 800], [721, 800], [720, 800], [461, 800], [460, 800], [320, 640], [860, 320]]
        for (const [width, height] of viewports) {
            await page.setViewportSize({ width, height })
            const box = await usage.boundingBox()
            const send = await page.getByRole('button', { name: '发送输入', exact: true }).boundingBox()
            const reasoning = await page.locator('.composer-reasoning-trigger').boundingBox()
            assert.ok(box && send && reasoning)
            assert.ok(box.x >= 0 && box.x + box.width <= width && box.y + box.height <= height)
            assert.ok(box.x + box.width <= send.x && Math.abs(box.y - send.y) <= 1)
            assert.ok(box.y > reasoning.y || box.x >= reasoning.x + reasoning.width)
            assert.equal(await usage.evaluate((element) => element.scrollWidth <= element.clientWidth), true)
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
            const requestLayout = await requests.nth(1).evaluate((element) => {
                const bounds = element.getBoundingClientRect()
                const parent = element.closest('.message-copy-actions').getBoundingClientRect()
                const copy = element.closest('.message-copy-actions').querySelector('.message-copy-button').getBoundingClientRect()
                const duration = element.previousElementSibling.getBoundingClientRect()
                return {
                    fits: bounds.left >= parent.left && bounds.right <= copy.left && element.scrollWidth <= element.clientWidth,
                    afterDuration: bounds.top >= duration.bottom || bounds.left >= duration.right,
                }
            })
            assert.deepEqual(requestLayout, { fits: true, afterDuration: true })
            await input.fill('测试输入')
            if (width === 1440 || width === 320) await page.screenshot({ path: join(tmpdir(), `praxis-token-usage-${process.pid}-${width}.png`) })
        }
        await page.emulateMedia({ reducedMotion: 'reduce' })
        await page.getByRole('button', { name: '思考强度: medium' }).click()
        await page.getByRole('slider', { name: '思考强度' }).waitFor({ state: 'visible' })
        await page.keyboard.press('Escape')
        assert.equal(await page.locator('.thinking-intensity-picker').count(), 0)

        // 流式请求和持久化消息必须显示同一请求用量，不因结算切换重复或丢失。
        await page.evaluate(() => {
            window.emitAgentEvent({ kind: 'operation_timing', agentId: 'primary', turnId: 'live-turn', timing: {
                ...window.timings[0], id: 'live-timing', turnId: 'live-turn', referenceId: 'assistant:live-turn:1',
            } })
            window.emitAgentEvent({ kind: 'text_delta', sessionId: 'session', agentId: 'primary', turnId: 'live-turn', step: 1, text: '实时模型响应' })
            window.emitAgentEvent({ kind: 'model_usage', sessionId: 'session', agentId: 'primary', turnId: 'live-turn', step: 1, usage: { inputTokens: 40, outputTokens: 10 } })
        })
        await page.waitForFunction(() => document.querySelector('.streaming-step .operation-token-usage')?.textContent === '· 消耗 50 tokens')
        await page.evaluate(() => {
            const at = '2026-01-01T00:00:00Z'
            window.agentHistory.push({ kind: 'message', sequence: 3, at, message: {
                id: 'assistant:live-turn:1', sequence: 3, at, turnId: 'live-turn', role: 'assistant', content: '实时模型响应',
            } })
            window.testAgent.turns.push({ id: 'live-turn', reason: 'user_input', status: 'settled', outcome: 'completed', failureCode: '', failureMessage: '', createdAt: at, startedAt: at, settledAt: at })
            window.usageRecords.push({ sessionId: 'session', agentId: 'primary', turnId: 'live-turn', step: 1, usage: { inputTokens: 40, outputTokens: 10 } })
            window.emitAgentEvent({ kind: 'settled', sessionId: 'session', agentId: 'primary', turnId: 'live-turn' })
        })
        await page.waitForFunction(() => !document.querySelector('.streaming-step') && document.querySelectorAll('.message-copy-actions .operation-token-usage')[2]?.textContent === '· 消耗 50 tokens')

        await page.reload()
        await page.waitForFunction(() => document.querySelector('.composer-token-usage')?.textContent === '150 tokens')
        assert.equal(await requests.nth(0).textContent(), '· 消耗 150 tokens')
        assert.equal(await requests.nth(1).textContent(), '· 消耗 -- tokens')
        await page.evaluate(() => {
            window.holdUsageQuery = true
            window.emitAgentEvent({ kind: 'settled', sessionId: 'session', agentId: 'primary', turnId: 'turn' })
        })
        await page.waitForFunction(() => typeof window.resolveUsageQuery === 'function')
        await page.evaluate(() => {
            window.emitAgentEvent({ kind: 'model_usage', sessionId: 'session', agentId: 'primary', turnId: 'turn', step: 1, usage: { inputTokens: 120, outputTokens: 100, cacheReadInputTokens: 80 } })
            window.resolveUsageQuery(window.usageRecords)
            window.holdUsageQuery = false
        })
        await page.waitForFunction(() => document.querySelector('.composer-token-usage')?.textContent === '220 tokens')
        assert.equal(await requests.nth(0).textContent(), '· 消耗 220 tokens')
        await page.evaluate(() => { window.usageLoadFails = true; window.emitAgentEvent({ kind: 'settled', sessionId: 'session', agentId: 'primary', turnId: 'turn' }) })
        await page.waitForFunction(() => document.querySelector('.composer-token-usage')?.textContent === '用量不可用')
        assert.match(await usage.getAttribute('title'), /加载失败/)
        assert.equal(await requests.nth(0).textContent(), '· 消耗 220 tokens')
        assert.equal(await requests.nth(1).textContent(), '· 消耗 用量不可用')
        await page.evaluate(() => { window.usageLoadFails = false; window.emitAgentEvent({ kind: 'settled', sessionId: 'session', agentId: 'primary', turnId: 'turn' }) })
        await page.waitForFunction(() => document.querySelector('.composer-token-usage')?.textContent === '220 tokens')
        await page.evaluate(() => window.emitAgentEvent({ kind: 'model_usage', sessionId: 'session', agentId: 'primary', turnId: 'turn', step: 2, usage: { inputTokens: 20 } }))
        await page.waitForFunction(() => document.querySelector('.composer-token-usage')?.textContent === '240+ tokens')
        assert.match(await usage.getAttribute('title'), /部分请求未上报/)
        assert.equal(await requests.nth(0).textContent(), '· 消耗 220 tokens')
        assert.equal(await requests.nth(1).textContent(), '· 消耗 20+ tokens')
        assert.match(await requests.nth(1).getAttribute('title'), /输出：未上报/)
        await page.evaluate(() => window.emitAgentEvent({ kind: 'model_usage', sessionId: 'session', agentId: 'primary', turnId: 'turn', step: 2, usage: { inputTokens: 20, outputTokens: 0 } }))
        await page.waitForFunction(() => document.querySelector('.composer-token-usage')?.textContent === '240 tokens')
        assert.equal(await requests.nth(1).textContent(), '· 消耗 20 tokens')
        assert.match(await requests.nth(1).getAttribute('title'), /输出：0 tokens/)
        assert.deepEqual(errors, [])
    } finally {
        await browser.close()
    }
})
