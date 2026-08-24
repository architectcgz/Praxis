import '../styles/workspace.css'
import {ProjectWorkspace} from '../features/projects'
import {WorkspaceErrorBoundary} from './WorkspaceErrorBoundary'

function App() {
	return (
		<WorkspaceErrorBoundary>
			<ProjectWorkspace />
		</WorkspaceErrorBoundary>
	)
}

export default App
