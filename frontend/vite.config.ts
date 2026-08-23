/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const backendTarget = process.env.BACKEND_URL || 'http://localhost:4500'

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/setupTests.ts'],
    exclude: ['node_modules/**', 'tests/e2e/**'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html'],
      exclude: ['node_modules/', 'src/**/*.d.ts', 'src/**/*.stories.*', 'src/**/__tests__/**'],
    },
  },
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
