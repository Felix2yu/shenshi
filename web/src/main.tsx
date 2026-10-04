import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import App from './App'
import { ErrorBoundary } from './components/ErrorBoundary'
import './index.css'
// 安装事件必须尽早监听：beforeinstallprompt 只在页面早期派发，
// 等组件挂载再挂监听常常已经错过。
import { captureInstallPrompt } from './lib/pwa'
import { AppProvider } from './store/AppStore'

captureInstallPrompt()

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
