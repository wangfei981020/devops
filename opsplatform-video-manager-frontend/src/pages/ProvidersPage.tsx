// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect, useMemo, useCallback } from 'react';
import { Button, Table, Space, message, Popconfirm, Input, Card, Statistic, Row, Col, Modal, Descriptions } from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, SearchOutlined, ReloadOutlined, EyeOutlined, ExportOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { providerAPI } from '../lib/api';
import type { CDNProvider } from '../lib/api';
import ProviderForm from '../components/ProviderForm';
import { getApiErrorMessage } from '../lib/httpError';

const { Search } = Input;

export default function ProvidersPage() {
  const [providers, setProviders] = useState<CDNProvider[]>([]);
  const [filteredProviders, setFilteredProviders] = useState<CDNProvider[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editingProvider, setEditingProvider] = useState<CDNProvider | null>(null);
  const [viewingProvider, setViewingProvider] = useState<CDNProvider | null>(null);
  const [searchText, setSearchText] = useState('');
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10 });

  const loadProviders = useCallback(async () => {
    try {
      setLoading(true);
      const data = await providerAPI.getAll();
      setProviders(data || []);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 providers'));
      setProviders([]);
    } finally {
      setLoading(false);
    }
  }, []);

  const filterProviders = useCallback(() => {
    if (!searchText.trim()) {
      setFilteredProviders(providers || []);
      return;
    }

    const filtered = (providers || []).filter(
      (provider) =>
        provider.name.toLowerCase().includes(searchText.toLowerCase()) ||
        provider.code.toLowerCase().includes(searchText.toLowerCase())
    );
    setFilteredProviders(filtered);
  }, [searchText, providers]);

  useEffect(() => {
    void loadProviders();
  }, [loadProviders]);

  useEffect(() => {
    filterProviders();
  }, [filterProviders]);

  const handleCreate = () => {
    setEditingProvider(null);
    setShowForm(true);
  };

  const handleEdit = (provider: CDNProvider) => {
    setEditingProvider(provider);
    setShowForm(true);
  };

  const handleView = async (id: number) => {
    try {
      const provider = await providerAPI.getById(id);
      setViewingProvider(provider);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 provider details'));
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await providerAPI.delete(id);
      message.success('Provider 删除成功');
      await loadProviders();
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '删除失败 provider'));
    }
  };

  const handleBatchDelete = async () => {
    if (selectedRowKeys.length === 0) {
      message.warning('请至少选择一个厂商');
      return;
    }

    let successCount = 0;
    let failCount = 0;
    const errors: string[] = [];

    for (const key of selectedRowKeys) {
      try {
        await providerAPI.delete(Number(key));
        successCount++;
      } catch (err: unknown) {
        failCount++;
        errors.push(`ID ${key}: ${getApiErrorMessage(err, '删除失败')}`);
      }
    }

    if (successCount > 0) {
      message.success(`成功删除 ${successCount} 个厂商`);
    }
    if (failCount > 0) {
      message.error(`删除失败 ${failCount} 个厂商：${errors.join('; ')}`);
    }

    setSelectedRowKeys([]);
    await loadProviders();
  };

  const handleFormSubmit = async () => {
    setShowForm(false);
    setEditingProvider(null);
    await loadProviders();
  };

  const handleExport = () => {
    const csvContent = [
      ['编号', '名称', '代码', '创建时间', '更新时间'].join(','),
      ...filteredProviders.map((p) =>
        [
          p.id,
          `"${p.name}"`,
          `"${p.code}"`,
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
    link.setAttribute('download', `cdn-providers-${new Date().toISOString().split('T')[0]}.csv`);
    link.style.visibility = 'hidden';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    message.success('Data 导出成功');
  };

  const rowSelection = {
    selectedRowKeys,
    onChange: (selectedKeys: React.Key[]) => {
      setSelectedRowKeys(selectedKeys);
    },
  };

  const columns: ColumnsType<CDNProvider> = [
    {
      title: 'ID',
      dataIndex: 'id',
      key: 'id',
      width: 80,
      sorter: (a, b) => a.id - b.id,
    },
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      sorter: (a, b) => a.name.localeCompare(b.name),
    },
    {
      title: '代码',
      dataIndex: 'code',
      key: 'code',
      sorter: (a, b) => a.code.localeCompare(b.code),
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
            title="删除厂商"
            description={`确定要删除 "${record.name}" (${record.code})?`}
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
  ];

  const stats = useMemo(() => {
    return {
      total: providers.length,
      filtered: filteredProviders.length,
    };
  }, [providers.length, filteredProviders.length]);

  return (
    <div className="vm-page">
      <Row gutter={16} className="vm-stat-row" style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic title="厂商总数" value={stats.total} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="筛选结果" value={stats.filtered} />
          </Card>
        </Col>
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
        <h2 className="vm-page-title">CDN 厂商</h2>
        <Space wrap>
          <Search
            placeholder="搜索 name or code"
            allowClear
            style={{ width: 250 }}
            prefix={<SearchOutlined />}
            onSearch={setSearchText}
            onChange={(e) => setSearchText(e.target.value)}
          />
          <Button
            icon={<ReloadOutlined />}
            onClick={loadProviders}
            loading={loading}
          >
            刷新
          </Button>
          <Button
            icon={<ExportOutlined />}
            onClick={handleExport}
            disabled={filteredProviders.length === 0}
          >
            导出
          </Button>
          {selectedRowKeys.length > 0 && (
            <Popconfirm
              title={`删除 ${selectedRowKeys.length} 厂商?`}
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
            创建 Provider
          </Button>
        </Space>
      </div>

      <Table
        columns={columns}
        dataSource={filteredProviders || []}
        rowKey="id"
        loading={loading}
        rowSelection={rowSelection}
        pagination={{
          current: pagination.current,
          pageSize: pagination.pageSize,
          showSizeChanger: true,
          showQuickJumper: true,
          showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条，共 ${total} 个厂商`,
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
        <ProviderForm
          provider={editingProvider}
          open={showForm}
          onClose={() => {
            setShowForm(false);
            setEditingProvider(null);
          }}
          onSubmit={handleFormSubmit}
          providers={providers}
        />
      )}

      <Modal
        title="厂商详情"
        open={viewingProvider !== null}
        onCancel={() => setViewingProvider(null)}
        footer={[
          <Button key="close" onClick={() => setViewingProvider(null)}>
            关闭
          </Button>,
          <Button
            key="edit"
            type="primary"
            onClick={() => {
              if (viewingProvider) {
                setViewingProvider(null);
                handleEdit(viewingProvider);
              }
            }}
          >
            编辑
          </Button>,
        ]}
      >
        {viewingProvider && (
          <Descriptions column={1} bordered>
            <Descriptions.Item label="ID">{viewingProvider.id}</Descriptions.Item>
            <Descriptions.Item label="名称">{viewingProvider.name}</Descriptions.Item>
            <Descriptions.Item label="代码">{viewingProvider.code}</Descriptions.Item>
            <Descriptions.Item label="创建时间">
              {new Date(viewingProvider.created_at).toLocaleString()}
            </Descriptions.Item>
            <Descriptions.Item label="更新时间">
              {new Date(viewingProvider.updated_at).toLocaleString()}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
}
