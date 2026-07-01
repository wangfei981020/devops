// Type declarations for react-dom/client
// This ensures TypeScript recognizes react-dom/client module when node_modules is not available

declare module 'react-dom/client' {
  import { ReactNode } from 'react';

  export interface Root {
    render(children: ReactNode): void;
    unmount(): void;
  }

  export function createRoot(container: Element | DocumentFragment): Root;
}

