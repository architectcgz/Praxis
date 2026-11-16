import { Cpu, FileJson, Settings2 } from 'lucide-react'
import type { ModelOption } from '../../api'

export function SettingsPanel({ models }: { models: ModelOption[] }) {
    return <section className="region settings-region" aria-labelledby="settings-title">
        <div className="scroll-region">
            <div className="page page-header settings-hero">
                <div className="settings-hero-content"><Settings2 size={28} /><div className="settings-hero-text">
                    <h1 id="settings-title">工作区设置</h1><p>配置文件决定模型、工具和审批默认值。</p>
                </div></div>
            </div>
            <div className="page settings-content">
                <div className="settings-card"><div className="settings-card-header"><Cpu size={22} /><h2>模型目录</h2></div>
                    <div className="settings-card-body"><p>当前会话可用 {models.length} 个模型。执行时使用的模型和推理等级在输入区选择。</p></div>
                </div>
                <div className="settings-info-card"><FileJson size={18} /><div className="settings-info-content">
                    <strong>编辑配置文件</strong>
                    <p>模型目录：<code>~/.praxis/config/models.json</code>。凭据和工具配置使用各自的配置文件。</p>
                    <p>保存后，在没有活动执行的会话中使用 <code>/reload</code>。重载只更新该会话，并清理尚未开始的队列。</p>
                </div></div>
            </div>
        </div>
    </section>
}
