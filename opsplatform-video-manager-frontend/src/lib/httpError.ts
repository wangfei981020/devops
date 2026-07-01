// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import type { AxiosError } from 'axios';

/**
 * Extracts a user-visible API error message from axios/network failures or generic Errors.
 *
 * @param error - Value caught from try/catch
 * @param fallback - Message when no structured detail is available
 * @returns Human-readable error string
 */
export function getApiErrorMessage(error: unknown, fallback: string): string {
  if (typeof error === 'object' && error !== null && 'response' in error) {
    const ax = error as AxiosError<{ message?: string }>;
    const msg = ax.response?.data?.message;
    if (typeof msg === 'string' && msg.length > 0) {
      return msg;
    }
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return fallback;
}

/** Ant Design Form.validateFields rejects with `{ errorFields: ... }` — not an API error */
export function isAntdFormValidateError(error: unknown): boolean {
  return typeof error === 'object' && error !== null && 'errorFields' in error;
}
