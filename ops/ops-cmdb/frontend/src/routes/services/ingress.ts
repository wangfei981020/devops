import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 入口层：Istio VirtualService / Gateway 与 Gateway API 的 HTTPRoute。
 *
 * ⚠️ 「服务与入口」页原来只列 Service —— 而**对外域名根本不在 Service 上**，
 * 它在 VirtualService 的 hosts 里。于是用 Istio 的环境在这一页看不到任何域名，
 * 而那恰恰是"这个域名打到哪个服务"这类排查的起点。
 */
export interface VirtualService {
  id?: number
  cluster_id?: number
  namespace?: string
  name?: string
  hosts?: string
  gateways?: string
  backends?: string
}

export interface Gateway {
  id?: number
  namespace?: string
  name?: string
  gateway_class?: string
  listeners?: string
  addresses?: string
  api_group?: string
  tls_secrets?: string
}

export interface HttpRoute {
  id?: number
  namespace?: string
  name?: string
  hostnames?: string
  parents?: string
  backends?: string
}

export function useVirtualServices(cid: number, enabled: boolean) {
  return useQuery({
    queryKey: ['k8s-virtualservices', cid],
    queryFn: () => apiGet<VirtualService[]>(`/api/k8s/virtualservices?cluster_id=${cid}`),
    enabled: enabled && cid > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useGateways(cid: number, enabled: boolean) {
  return useQuery({
    queryKey: ['k8s-gateways', cid],
    queryFn: () => apiGet<Gateway[]>(`/api/k8s/gateways?cluster_id=${cid}`),
    enabled: enabled && cid > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useHttpRoutes(cid: number, enabled: boolean) {
  return useQuery({
    queryKey: ['k8s-httproutes', cid],
    queryFn: () => apiGet<HttpRoute[]>(`/api/k8s/httproutes?cluster_id=${cid}`),
    enabled: enabled && cid > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

/**
 * 原生 Ingress。
 *
 * ⚠️ 这一层以前**漏了**：弹窗里有 VirtualService / Gateway / HTTPRoute 三档，
 * 唯独没有最基础的 Ingress（OPSCMDB-023 第二档）。
 * 后果是用原生 Ingress 的集群在「入口与域名」里一条都看不到 ——
 * 而那正是这个弹窗存在的理由（"对外域名不在 Service 上"）。
 */
export interface Ingress {
  id: number
  namespace: string
  name: string
  /** 逗号分隔，与 VS/Gateway 同一形态 */
  hosts?: string
  tls?: string
  svc_names?: string
}

export function useIngresses(cid: number, enabled: boolean) {
  return useQuery({
    queryKey: ['k8s-ingresses', cid],
    queryFn: () => apiGet<Ingress[]>(`/api/k8s/ingresses?cluster_id=${cid}`),
    enabled: enabled && cid > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
