import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import App from './App'
import { ErrorBoundary } from './components/ErrorBoundary'
import './index.css'
import { AppProvider } from './store/AppStore'

const host = document.getElementById('root')
if (!host) throw new Error('缺少 #root 挂载点')

createRoot(host).render(
  <StrictMode>
    <ErrorBoundary>
      <AppProvider>
        <App />
      </AppProvider>
    </ErrorBoundary>
  </StrictMode>,
)
