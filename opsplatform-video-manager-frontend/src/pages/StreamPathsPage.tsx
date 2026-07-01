// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect, useMemo, useCallback } from 'react';
import { Button, Table, Space, Select, message, Popconfirm, Input, Card, Statistic, Row, Col, Modal, Descriptions, Typography } from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, SearchOutlined, ReloadOutlined, EyeOutlined, ExportOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { streamPathAPI, streamAPI } from '../lib/api';
import type { StreamPath, Stream, StreamPathImportResult } from '../lib/api';
import { selectSearchableProps } from '../lib/selectSearchProps';
import { displayStreamSeries, streamSeriesLabel } from '../lib/streamSeries';
import StreamPathForm from '../components/StreamPathForm';
import { getApiErrorMessage } from '../lib/httpError';

const { Search } = Input;

export default function StreamPathsPage() {
  const [paths, setPaths] = useState<StreamPath[]>([]);
  const [filteredPaths, setFilteredPaths] = useState<StreamPath[]>([]);
  const [streams, setStreams] = useState<Stream[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editingPath, setEditingPath] = useState<StreamPath | null>(null);
  const [viewingPath, setViewingPath] = useState<StreamPath | null>(null);
  const [filterStreamId, setFilterStreamId] = useState<number | undefined>(undefined);
  const [filterSeries, setFilterSeries] = useState<string | undefined>(undefined);
  const [searchText, setSearchText] = useState('');
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10 });
  const [importOpen, setImportOpen] = useState(false);
  const [importFile, setImportFile] = useState<File | null>(null);
  const [importing, setImporting] = useState(false);
  const [importSummaryOpen, setImportSummaryOpen] = useState(false);
  const [importSummaryText, setImportSummaryText] = useState('');

  const loadStreams = useCallback(async () => {
    try {
      const data = await streamAPI.getAll();
      setStreams(data || []);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 streams'));
      setStreams([]);
    }
  }, []);

  const loadPaths = useCallback(async () => {
    try {
      setLoading(true);
      const data = await streamPathAPI.getAll(filterStreamId);
      setPaths(data || []);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 stream paths'));
      setPaths([]);
    } finally {
      setLoading(false);
    }
  }, [filterStreamId]);

  const filterPaths = useCallback(() => {
    let filtered = paths || [];

    if (filterSeries) {
      filtered = filtered.filter(
        (path) => displayStreamSeries(path) === filterSeries
      );
    }

    if (searchText.trim()) {
      const q = searchText.toLowerCase();
      filtered = filtered.filter((path) => {
        const seriesQ = (path.series || streamSeriesLabel(path.table_id)).toLowerCase();
        return (
          path.table_id.toLowerCase().includes(q) ||
          path.full_path.toLowerCase().includes(q) ||
          path.stream?.name.toLowerCase().includes(q) ||
          seriesQ.includes(q) ||
          displayStreamSeries(path).toLowerCase().includes(q)
        );
      });
    }

    setFilteredPaths(filtered);
  }, [searchText, paths, filterSeries]);

  useEffect(() => {
    void loadStreams();
  }, [loadStreams]);

  useEffect(() => {
    void loadPaths();
  }, [loadPaths]);

  useEffect(() => {
    filterPaths();
  }, [filterPaths]);

  const handleCreate = () => {
    setEditingPath(null);
    setShowForm(true);
  };

  const handleEdit = useCallback((path: StreamPath) => {
    setEditingPath(path);
    setShowForm(true);
  }, []);

  const handleView = useCallback(async (id: number) => {
    try {
      const path = await streamPathAPI.getById(id);
      setViewingPath(path);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 stream path details'));
    }
  }, []);

  const handleDelete = useCallback(
    async (id: number) => {
      try {
        await streamPathAPI.delete(id);
        message.success('Stream path 删除成功');
        await loadPaths();
      } catch (err: unknown) {
        message.error(getApiErrorMessage(err, '删除失败 stream path'));
      }
    },
    [loadPaths]
  );

  const handleBatchDelete = useCallback(async () => {
    if (selectedRowKeys.length === 0) {
      message.warning('请至少选择一个流路径');
      return;
    }

    let successCount = 0;
    let failCount = 0;
    const errors: string[] = [];

    for (const key of selectedRowKeys) {
      try {
        await streamPathAPI.delete(Number(key));
        successCount++;
      } catch (err: unknown) {
        failCount++;
        errors.push(`ID ${key}: ${getApiErrorMessage(err, '删除失败')}`);
      }
    }

    if (successCount > 0) {
      message.success(`成功删除 ${successCount} 个流路径`);
    }
    if (failCount > 0) {
      message.error(`删除失败 ${failCount} 个流路径：${errors.join('; ')}`);
    }

    setSelectedRowKeys([]);
    await loadPaths();
  }, [selectedRowKeys, loadPaths]);

  const handleFormSubmit = async () => {
    setShowForm(false);
    setEditingPath(null);
    await loadPaths();
  };

  const handleExport = () => {
    const csvContent = [
      ['编号', '桌台号', '系列', '路径', '流区域', '创建时间', '更新时间'].join(','),
      ...filteredPaths.map((p) =>
        [
          p.id,
          `"${p.table_id}"`,
          `"${displayStreamSeries(p)}"`,
          `"${p.full_path}"`,
          `"${p.stream?.name || 'Unknown'}"`,
          new Date(p.created_at).toISOString(),
          new Date(p.updated_at).toISOString(),
        ].join(',')
      ),
    ].join('\n');

    // Add UTF-8 BOM to fix Chinese character encoding issues in Excel
    const blob = new Blob(['\uFEFF' + csvContent], { type: 'text/csv;charset=utf-8;' });
    const link = document.createElement('a');
    const url = URL.createObjectURL(blob);
    link.setAttribute('href', url);
    link.setAttribute('download', `stream-paths-${new Date().toISOString().split('T')[0]}.csv`);
    link.style.visibility = 'hidden';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    message.success('Data 导出成功');
  };

  const downloadImportTemplate = () => {
    const header = ['桌台号', '路径', '流区域'].join(',');
    const example = ['T01', '/live/example/stream', '与「视频流区域」名称一致'].join(',');
    const csv = `\uFEFF${header}\n${example}\n`;
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const link = document.createElement('a');
    link.href = URL.createObjectURL(blob);
    link.download = 'stream-paths-import-template.csv';
    link.click();
    URL.revokeObjectURL(link.href);
  };

  const showImportSummary = (result: StreamPathImportResult) => {
    const lines = [
      `新增 ${result.created} 条`,
      `更新 ${result.updated} 条`,
    ];
    if (result.errors.length > 0) {
      lines.push('', '失败行：');
      result.errors.slice(0, 30).forEach((e) => {
        lines.push(`第 ${e.line} 行${e.table_id ? `（${e.table_id}）` : ''}: ${e.message}`);
      });
      if (result.errors.length > 30) {
        lines.push(`… 另有 ${result.errors.length - 30} 条错误未显示`);
      }
    }
    setImportSummaryText(lines.join('\n'));
    setImportSummaryOpen(true);
  };

  const runImport = async () => {
    if (!importFile) {
      message.warning('请选择 CSV 文件');
      return;
    }
    setImporting(true);
    try {
      const result = await streamPathAPI.importCsv(importFile);
      if (result.errors.length === 0) {
        message.success(`新增 ${result.created} 条，更新 ${result.updated} 条`);
      } else {
        if (result.created === 0 && result.updated === 0) {
          message.error(`导入未写入任何行，共 ${result.errors.length} 处错误`);
        } else {
          message.warning(`新增 ${result.created} 条，更新 ${result.updated} 条；${result.errors.length} 行失败`);
        }
        showImportSummary(result);
      }
      setImportOpen(false);
      setImportFile(null);
      await loadPaths();
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '导入失败'));
    } finally {
      setImporting(false);
    }
  };

  const rowSelection = useMemo(() => ({
    selectedRowKeys,
    onChange: (selectedKeys: React.Key[]) => {
      setSelectedRowKeys(selectedKeys);
    },
  }), [selectedRowKeys]);

  const seriesFilterOptions = useMemo(() => {
    const set = new Set<string>();
    for (const p of paths) {
      set.add(displayStreamSeries(p));
    }
    return Array.from(set).sort((a, b) => a.localeCompare(b, 'zh-CN'));
  }, [paths]);

  const columns: ColumnsType<StreamPath> = useMemo(() => [
    {
      title: 'ID',
      dataIndex: 'id',
      key: 'id',
      width: 80,
      sorter: (a, b) => a.id - b.id,
    },
    {
      title: '桌台号',
      dataIndex: 'table_id',
      key: 'table_id',
      sorter: (a, b) => a.table_id.localeCompare(b.table_id),
    },
    {
      title: '系列',
      key: 'series',
      width: 120,
      render: (_, record) => displayStreamSeries(record),
      sorter: (a, b) =>
        displayStreamSeries(a).localeCompare(displayStreamSeries(b), 'zh-CN'),
    },
    {
      title: '路径',
      dataIndex: 'full_path',
      key: 'full_path',
      sorter: (a, b) => a.full_path.localeCompare(b.full_path),
    },
    {
      title: '视频流区域',
      key: 'stream',
      render: (_, record) => record.stream?.name || 'Unknown',
      filters: (streams || []).map((s) => ({ text: s.name, value: s.id })),
      onFilter: (value, record) => record.stream_id === value,
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (date: string) => new Date(date).toLocaleString(),
      sorter: (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
    },
    {
      title: '操作',
      key: 'actions',
      width: 200,
      render: (_, record) => (
        <Space>
          <Button
            type="link"
            icon={<EyeOutlined />}
            onClick={() => handleView(record.id)}
          >
            查看
          </Button>
          <Button
            type="link"
            icon={<EditOutlined />}
            onClick={() => handleEdit(record)}
          >
            编辑
          </Button>
          <Popconfirm
            title="删除流路径"
            description={`确定要删除 "${record.table_id}" (${record.full_path})?`}
            onConfirm={() => handleDelete(record.id)}
            okText="确定"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Button
              type="link"
              danger
              icon={<DeleteOutlined />}
            >
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ], [streams, handleView, handleEdit, handleDelete]);

  const stats = useMemo(() => {
    return {
      total: paths.length,
      filtered: filteredPaths.length,
      byStream: filterStreamId
        ? paths.filter((p) => p.stream_id === filterStreamId).length
        : undefined,
    };
  }, [paths, filteredPaths, filterStreamId]);

  return (
    <div className="vm-page">
      <Row gutter={16} className="vm-stat-row" style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic title="Total Paths" value={stats.total} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="筛选结果" value={stats.filtered} />
          </Card>
        </Col>
        {stats.byStream !== undefined && (
          <Col span={6}>
            <Card>
              <Statistic title="按视频流区域筛选" value={stats.byStream} />
            </Card>
          </Col>
        )}
      </Row>

      <div
        className="vm-toolbar-panel"
        style={{
          marginBottom: 16,
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 12,
        }}
      >
        <h2 className="vm-page-title">流路径</h2>
        <Space wrap>
          <Search
            placeholder="搜索桌台号、系列、路径或视频流区域"
            allowClear
            style={{ width: 280 }}
            prefix={<SearchOutlined />}
            onSearch={setSearchText}
            onChange={(e) => setSearchText(e.target.value)}
          />
          <Select
            placeholder="筛选系列"
            allowClear
            style={{ width: 140 }}
            value={filterSeries}
            onChange={(v) => setFilterSeries(v)}
            {...selectSearchableProps}
          >
            {seriesFilterOptions.map((s) => (
              <Select.Option key={s} value={s} label={s}>
                {s}
              </Select.Option>
            ))}
          </Select>
          <Select
            placeholder="筛选视频流区域"
            allowClear
            style={{ width: 200 }}
            onChange={(value) => setFilterStreamId(value)}
            value={filterStreamId}
            {...selectSearchableProps}
          >
            {(streams || []).map((stream) => (
              <Select.Option
                key={stream.id}
                value={stream.id}
                label={`${stream.name} ${stream.code}`}
              >
                {stream.name} ({stream.code})
              </Select.Option>
            ))}
          </Select>
          <Button
            icon={<ReloadOutlined />}
            onClick={loadPaths}
            loading={loading}
          >
            刷新
          </Button>
          <Button
            icon={<ExportOutlined />}
            onClick={handleExport}
            disabled={filteredPaths.length === 0}
          >
            导出
          </Button>
          <Button onClick={() => setImportOpen(true)}>导入</Button>
          {selectedRowKeys.length > 0 && (
            <Popconfirm
              title={`删除 ${selectedRowKeys.length} 流路径?`}
              description="此操作无法撤销."
              onConfirm={handleBatchDelete}
              okText="是"
              cancelText="否"
              okButtonProps={{ danger: true }}
            >
              <Button danger icon={<DeleteOutlined />}>
                删除 Selected ({selectedRowKeys.length})
              </Button>
            </Popconfirm>
          )}
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={handleCreate}
          >
            创建 Path
          </Button>
        </Space>
      </div>

      <Table
        columns={columns}
        dataSource={filteredPaths || []}
        rowKey="id"
        loading={loading}
        rowSelection={rowSelection}
        pagination={{
          current: pagination.current,
          pageSize: pagination.pageSize,
          showSizeChanger: true,
          showQuickJumper: true,
          showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条，共 ${total} 个流路径`,
          pageSizeOptions: ['10', '20', '50', '100'],
          onChange: (page, size) => {
            setPagination({ current: page, pageSize: size });
          },
          onShowSizeChange: (_current, size) => {
            setPagination({ current: 1, pageSize: size });
          },
        }}
        scroll={{ x: 'max-content' }}
      />

      {showForm && (
        <StreamPathForm
          path={editingPath}
          streams={streams}
          paths={paths}
          open={showForm}
          onClose={() => {
            setShowForm(false);
            setEditingPath(null);
          }}
          onSubmit={handleFormSubmit}
        />
      )}

      <Modal
        title="批量导入流路径"
        open={importOpen}
        onCancel={() => {
          setImportOpen(false);
          setImportFile(null);
        }}
        footer={[
          <Button key="tpl" onClick={downloadImportTemplate}>
            下载模板
          </Button>,
          <Button key="cancel" onClick={() => { setImportOpen(false); setImportFile(null); }}>
            取消
          </Button>,
          <Button key="ok" type="primary" loading={importing} onClick={runImport}>
            开始导入
          </Button>,
        ]}
        destroyOnClose
      >
        <Typography.Paragraph type="secondary" style={{ marginBottom: 12 }}>
          以<strong>桌台号</strong>为唯一键：已存在则更新路径与视频流区域，否则新增。支持与导出相同的列（编号、创建时间等列可忽略）。
          必填列：<strong>桌台号</strong>、<strong>路径</strong>，以及 <strong>流区域</strong>（名称，与列表一致）或 <strong>stream_id</strong>（数字）。
        </Typography.Paragraph>
        <input
          type="file"
          accept=".csv,text/csv"
          style={{ marginBottom: 8 }}
          onChange={(e) => {
            const f = e.target.files?.[0];
            setImportFile(f ?? null);
          }}
        />
        {importFile ? (
          <Typography.Text type="secondary">已选择：{importFile.name}</Typography.Text>
        ) : null}
      </Modal>

      <Modal
        title="导入结果"
        open={importSummaryOpen}
        onCancel={() => setImportSummaryOpen(false)}
        footer={[
          <Button key="ok" type="primary" onClick={() => setImportSummaryOpen(false)}>
            确定
          </Button>,
        ]}
        width={560}
      >
        <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', marginBottom: 0 }}>
          {importSummaryText}
        </Typography.Paragraph>
      </Modal>

      <Modal
        title="流路径详情"
        open={viewingPath !== null}
        onCancel={() => setViewingPath(null)}
        footer={[
          <Button key="close" onClick={() => setViewingPath(null)}>
            关闭
          </Button>,
          <Button
            key="edit"
            type="primary"
            onClick={() => {
              if (viewingPath) {
                setViewingPath(null);
                handleEdit(viewingPath);
              }
            }}
          >
            编辑
          </Button>,
        ]}
        width={600}
      >
        {viewingPath && (
          <Descriptions column={1} bordered>
            <Descriptions.Item label="ID">{viewingPath.id}</Descriptions.Item>
            <Descriptions.Item label="桌台号">{viewingPath.table_id}</Descriptions.Item>
            <Descriptions.Item label="系列">{displayStreamSeries(viewingPath)}</Descriptions.Item>
            <Descriptions.Item label="路径">{viewingPath.full_path}</Descriptions.Item>
            <Descriptions.Item label="视频流区域">
              {viewingPath.stream?.name || 'Unknown'} ({viewingPath.stream?.code || 'N/A'})
            </Descriptions.Item>
            <Descriptions.Item label="Stream ID">{viewingPath.stream_id}</Descriptions.Item>
            <Descriptions.Item label="创建时间">
              {new Date(viewingPath.created_at).toLocaleString()}
            </Descriptions.Item>
            <Descriptions.Item label="更新时间">
              {new Date(viewingPath.updated_at).toLocaleString()}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
}

