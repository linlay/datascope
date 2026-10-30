import { defineConfig } from 'vite';

// The frontend is its own service; all data comes from the Go admin API.
const adminApi = process.env.ADMIN_API_URL ?? 'http://127.0.0.1:8081';
const port = Number(process.env.WEB_PORT ?? 8082);
// Keep the browser's Host header (Vite's string shorthand would rewrite it) so
// the admin API's same-origin and DNS-rebinding checks see the real origin.
const target = { target: adminApi, changeOrigin: false };
const proxy = { '/api': target, '/actions': target, '/healthz': target };

export default defineConfig({
  esbuild: { jsx: 'automatic' },
  server: { host: '127.0.0.1', port, strictPort: true, proxy },
  preview: { host: '127.0.0.1', port, strictPort: true, proxy },
  build: {
    chunkSizeWarningLimit: 2000,
    rollupOptions: {
      // antd ships "use client" directives, which are meaningless in an SPA bundle.
      onwarn(warning, warn) {
        if (warning.code !== 'MODULE_LEVEL_DIRECTIVE') warn(warning);
      },
    },
  },
});
