// Type declarations for CSS modules and CSS imports
// This ensures TypeScript recognizes CSS file imports

declare module '*.css' {
  const content: string;
  export default content;
}

declare module 'antd/dist/reset.css' {
  const content: string;
  export default content;
}

