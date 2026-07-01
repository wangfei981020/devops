// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

/**
 * 与后端 `StreamSeriesLabel` 一致：桌台号开头连续字母 +「系列」（如 L001 -> L系列）。
 */
export function streamSeriesLabel(tableId: string): string {
  let prefix = '';
  for (const ch of tableId) {
    if (/\p{L}/u.test(ch)) {
      prefix += ch;
    } else {
      break;
    }
  }
  return prefix ? `${prefix}系列` : '';
}

/** 展示用：优先使用 API 的 series，否则本地推导 */
export function displayStreamSeries(path: { series?: string; table_id: string }): string {
  if (path.series) {
    return path.series;
  }
  const derived = streamSeriesLabel(path.table_id);
  return derived || '—';
}
