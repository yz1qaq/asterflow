import React from 'react'
import ReactDOM from 'react-dom/client'
import { App as AntApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'
import 'antd/dist/reset.css'
import './styles.css'

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ConfigProvider locale={zhCN} theme={{ token: { colorPrimary: '#3f5fce', colorInfo: '#3f5fce', colorSuccess: '#287956', colorText: '#24314d', colorTextSecondary: '#62718a', colorBorder: '#e4e9f4', borderRadius: 10, controlHeight: 36, fontSize: 14, fontFamily: '-apple-system, BlinkMacSystemFont, Segoe UI, PingFang SC, Microsoft YaHei, sans-serif' } }}>
      <QueryClientProvider client={queryClient}>
        <AntApp><App /></AntApp>
      </QueryClientProvider>
    </ConfigProvider>
  </React.StrictMode>,
)
