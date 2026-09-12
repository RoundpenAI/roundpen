import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
// Package exports block dist/css; import via relative path for Vite.
import '../node_modules/@douyinfe/semi-ui-19/dist/css/semi.css'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
