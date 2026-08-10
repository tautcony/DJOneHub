import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    // The current management shell and Ant Design base components share one entry.
    // Keep the warning threshold explicit until the bundle is split by feature.
    chunkSizeWarningLimit: 1200,
  },
  server: {
    port: 5176,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:7575',
        ws: true,
        changeOrigin: true,
        rewriteWsOrigin: true,
      },
    },
  },
})
