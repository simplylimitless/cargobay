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
      '/docker': {
        target: backendTarget,
        changeOrigin: true,
      },
    },
  },
})
