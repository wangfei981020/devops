# 本地字体目录

此目录存放本地字体文件，避免依赖外网 Google Fonts。

## 当前字体

| 字体 | 字重 | 文件 |
|------|------|------|
| Inter | 300 (Light) | Inter-Light.woff2 |
| Inter | 400 (Regular) | Inter-Regular.woff2 |
| Inter | 500 (Medium) | Inter-Medium.woff2 |
| Inter | 600 (SemiBold) | Inter-SemiBold.woff2 |
| Inter | 700 (Bold) | Inter-Bold.woff2 |

## 使用方式

在 HTML 中引用 `fonts.css`：

```html
<link rel="stylesheet" href="/fonts/fonts.css">
```

CSS 中使用：

```css
font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
```

## 目录结构

```
fonts/
├── README.md           # 本文件
├── fonts.css           # 字体定义
├── Inter-Light.woff2   # 字重 300
├── Inter-Regular.woff2 # 字重 400
├── Inter-Medium.woff2  # 字重 500
├── Inter-SemiBold.woff2 # 字重 600
└── Inter-Bold.woff2    # 字重 700
```

## 添加其他字体

1. 将 `.woff2` 文件放入此目录
2. 在 `fonts.css` 中添加 `@font-face` 规则
3. 更新 HTML 引用
