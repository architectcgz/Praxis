import './App.css';

function App() {
    return (
        <main className="shell">
            <p className="eyebrow">DESKTOP ORCHESTRATION WORKSPACE</p>
            <h1>Praxis</h1>
            <p className="subtitle">P0 scaffold is ready for the application runtime.</p>
            <section className="status-panel" aria-label="Runtime status">
                <span className="status-dot" aria-hidden="true" />
                <div>
                    <strong>Runtime scaffold ready</strong>
                    <span>Wails, React, and the module boundaries are in place.</span>
                </div>
            </section>
        </main>
    )
}

export default App
