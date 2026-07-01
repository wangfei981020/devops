// Type declarations for react
// This ensures TypeScript recognizes react module when node_modules is not available

declare module 'react' {
  export interface ReactElement<P = any, T extends string | JSX.ElementConstructor<any> = string | JSX.ElementConstructor<any>> {
    type: T;
    props: P;
    key: Key | null;
  }

  export type Key = string | number;
  export type ReactNode = ReactElement | string | number | boolean | null | undefined | ReactNode[];
  export type Dispatch<A> = (value: A) => void;
  export type SetStateAction<S> = S | ((prevState: S) => S);

  export interface Component<P = {}, S = {}, SS = any> {
    props: P;
    state: S;
  }

  export interface ComponentType<P = {}> {
    (props: P, context?: any): ReactElement | null;
  }

  export interface ExoticComponent<P = {}> {
    (props: P): ReactElement | null;
  }

  export interface JSXElementConstructor<P> {
    (props: P): ReactElement | null;
  }

  // React Hooks
  export function useState<S>(initialState: S | (() => S)): [S, Dispatch<SetStateAction<S>>];
  export function useState<S = undefined>(): [S | undefined, Dispatch<SetStateAction<S | undefined>>];
  
  export function useEffect(effect: () => void | (() => void), deps?: any[]): void;
  export function useMemo<T>(factory: () => T, deps: any[]): T;
  export function useCallback<T extends (...args: any[]) => any>(callback: T, deps: any[]): T;
  export function useRef<T>(initialValue: T): { current: T };
  export function useRef<T>(initialValue: T | null): { current: T | null };
  export function useRef<T = undefined>(): { current: T | undefined };

  export const StrictMode: ComponentType<{ children?: ReactNode }>;
  export const Fragment: ExoticComponent<{ children?: ReactNode }>;

  // JSX namespace for type checking
  namespace JSX {
    interface Element extends ReactElement<any, any> {}
    interface ElementClass extends Component<any> {}
    interface ElementAttributesProperty {
      props: {};
    }
    interface ElementChildrenAttribute {
      children: {};
    }
    interface IntrinsicElements {
      [elemName: string]: any;
    }
  }
}

