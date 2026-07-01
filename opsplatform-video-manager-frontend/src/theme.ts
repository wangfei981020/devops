// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import type { ThemeConfig } from 'antd';

/**
 * 控制台主题：深侧栏 + 青绿主色，偏广电/播出控制台气质。
 */
export const appTheme: ThemeConfig = {
  token: {
    colorPrimary: '#0d9488',
    colorInfo: '#0284c7',
    colorSuccess: '#059669',
    colorWarning: '#d97706',
    colorError: '#dc2626',
    borderRadius: 10,
    fontFamily:
      "'Plus Jakarta Sans', 'Noto Sans SC', 'PingFang SC', 'Microsoft YaHei', system-ui, sans-serif",
    colorBgContainer: '#ffffff',
    colorBgLayout: '#e8edf3',
    colorBorderSecondary: 'rgba(15, 23, 42, 0.08)',
    colorText: 'rgba(15, 23, 42, 0.92)',
    colorTextSecondary: 'rgba(71, 85, 105, 0.95)',
  },
  components: {
    Card: {
      borderRadiusLG: 12,
    },
    Table: {
      headerBg: '#f1f5f9',
      headerColor: 'rgba(15, 23, 42, 0.88)',
      rowHoverBg: 'rgba(13, 148, 136, 0.06)',
      borderColor: 'rgba(15, 23, 42, 0.06)',
    },
    Menu: {
      darkItemBg: 'transparent',
      darkItemSelectedBg: 'rgba(45, 212, 191, 0.12)',
      darkItemHoverBg: 'rgba(255, 255, 255, 0.06)',
      darkSubMenuItemBg: 'transparent',
    },
    Button: {
      primaryShadow: '0 1px 2px rgba(13, 148, 136, 0.2)',
    },
    Layout: {
      bodyBg: '#e8edf3',
    },
  },
};
