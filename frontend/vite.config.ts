import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const backendTarget = process.env.BACKEND_URL || 'http://localhost:4500'

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    proxy: {
      '/api': {
        target: backendTarget,
        changeOrigin: true,
      },
      '/npm': {
        target: backendTarget,
        changeOrigin: true,
      },
      '/maven': {
        target: backendTarget,
        changeOrigin: true,
      },
      '/v2': {
        target: backendTarget,
        changeOrigin: true,
      },
      '/pypi': {
        target: backendTarget,
        changeOrigin: true,
      },
      '/nuget': {
        target: backendTarget,
        changeOrigin: true,
      },
      '/helm': {
        target: backendTarget,
        changeOrigin: true,
      },
    },
  },
})
