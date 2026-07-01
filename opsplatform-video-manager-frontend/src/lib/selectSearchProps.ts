// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

/**
 * Ant Design Select：允许在下拉中输入以筛选选项。
 * 每个 {@link import('antd').Select.Option} 需设置 `label` 为参与匹配的文案（可与展示内容一致）。
 */
export const selectSearchableProps = {
  showSearch: true,
  optionFilterProp: 'label' as const,
};
