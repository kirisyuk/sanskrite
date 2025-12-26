import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwind from '@tailwindcss/vite';

export default defineConfig({
  plugins: [react(), tailwind()],
  optimizeDeps: {
    include: ['react', 'react-dom', 'react/jsx-runtime'],
    esbuildOptions: {
      jsx: 'automatic',
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    // Dev-only proxy so `/api/*` requests can be served by the Go backend without CORS pain.
    // This is useful when VITE_API_BASE_URL is unset/empty (relative requests) or when you want
    // the frontend to always talk to the local backend during `vite dev`.
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
      // Backend websocket endpoint (if used locally)
      '/ws': {
        target: 'http://localhost:8080',
        ws: true,
        changeOrigin: true,
        secure: false,
      },
    },
    watch: {
      ignored: ['**/node_modules/**', '**/dist/**', '**/.git/**'],
    },
  },
});
