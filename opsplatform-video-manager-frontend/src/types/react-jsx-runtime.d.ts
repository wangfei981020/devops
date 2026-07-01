// Type declarations for react/jsx-runtime
// This ensures TypeScript recognizes the JSX runtime module
// These types are provided when @types/react is installed, but we define them here
// as a fallback when node_modules is not available

declare module 'react/jsx-runtime' {
  export function jsx(
    type: any,
    props: any,
    key?: string | number | null
  ): any;

  export function jsxs(
    type: any,
    props: any,
    key?: string | number | null
  ): any;

  export const Fragment: any;
}

declare module 'react/jsx-dev-runtime' {
  export function jsxDEV(
    type: any,
    props: any,
    key?: string | number | null,
    isStaticChildren?: boolean,
    source?: {
      fileName: string;
      lineNumber: number;
      columnNumber: number;
    },
    self?: any
  ): any;

  export const Fragment: any;
}

