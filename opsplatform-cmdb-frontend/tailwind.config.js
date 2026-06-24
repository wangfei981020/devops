/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js}'],
  // 禁用 preflight，避免 Tailwind 的 reset 破坏 Element Plus 组件样式
  corePlugins: { preflight: false },
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'Segoe UI', 'Microsoft YaHei', 'sans-serif'],
        mono: ['JetBrains Mono', 'Consolas', 'Menlo', 'monospace'],
      },
      colors: {
        brand: { DEFAULT: '#4f46e5', hover: '#4338ca', soft: '#eef2ff' },
      },
    },
  },
  plugins: [],
}
