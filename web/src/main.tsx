// React 19: inject createRoot for Modal.confirm / Toast / Notification.
import '@douyinfe/semi-ui-19/react19-adapter'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { SemiAppProvider } from './components/SemiAppProvider'
import App from './App'
import { installVisualViewportInsets } from './lib/visualViewport'
import { registerServiceWorker } from './lib/pushNotifications'
// Package exports block dist/css; import via relative path for Vite.
import '../node_modules/@douyinfe/semi-ui-19/dist/css/semi.css'
import './fonts/ibm-plex-mono.css'
import './index.css'

installVisualViewportInsets()
void registerServiceWorker().catch(() => undefined)

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <SemiAppProvider>
      <App />
    </SemiAppProvider>
  </StrictMode>,
)
