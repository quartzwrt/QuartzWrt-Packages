import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import tailwindcss from '@tailwindcss/vite';

/*
 * 产物：IIFE 单文件 app.js + app.css（提交入库，固件构建零 node 依赖）
 * 入口契约：window.QuartzDashboard.mount(el, host)（见 ../htdocs/.../view/quartz/dashboard.js 壳层）
 * Tailwind v4 隔离：qz: 前缀 + source(none)，主题 cascade 入层（见 styles/app.css）
 */
export default defineConfig({
  plugins: [vue(), tailwindcss()],

  define: {
    /* lib 模式 Vite 不替换 NODE_ENV：必须显式定义，否则浏览器端 process 未定义直接抛错 */
    'process.env.NODE_ENV': JSON.stringify('production'),
    __VUE_OPTIONS_API__: false,
    __VUE_PROD_DEVTOOLS__: false,
    __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: false
  },

  build: {
    outDir: '../root/www/luci-static/resources/quartz-dashboard',
    emptyOutDir: true,
    target: 'es2019',
    cssCodeSplit: false,
    minify: 'esbuild',
    sourcemap: false,
    reportCompressedSize: true,
    lib: {
      entry: 'src/main.js',
      name: 'QuartzDashboard',
      formats: ['iife'],
      fileName: () => 'app.js',
      cssFileName: 'app'
    },
    rollupOptions: {
      output: { inlineDynamicImports: true }
    }
  }
});
