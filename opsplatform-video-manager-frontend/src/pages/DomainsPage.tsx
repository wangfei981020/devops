// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect, useMemo, useCallback } from 'react';
import { Button, Table, Space, message, Popconfirm, Input, Card, Statistic, Row, Col, Modal, Descriptions } from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, SearchOutlined, ReloadOutlined, EyeOutlined, ExportOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { domainAPI } from '../lib/api';
import type { Domain } from '../lib/api';
import DomainForm from '../components/DomainForm';
import { getApiErrorMessage } from '../lib/httpError';

const { Search } = Input;

export default function DomainsPage() {
  const [domains, setDomains] = useState<Domain[]>([]);
  const [filteredDomains, setFilteredDomains] = useState<Domain[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editingDomain, setEditingDomain] = useState<Domain | null>(null);
  const [viewingDomain, setViewingDomain] = useState<Domain | null>(null);
  const [searchText, setSearchText] = useState('');
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10 });

  const loadDomains = useCallback(async () => {
    try {
      setLoading(true);
      const data = await domainAPI.getAll();
      setDomains(data || []);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 domains'));
      setDomains([]);
    } finally {
      setLoading(false);
    }
  }, []);

  const filterDomains = useCallback(() => {
    if (!searchText.trim()) {
      setFilteredDomains(domains || []);
      return;
    }

    const filtered = (domains || []).filter(
      (domain) => domain.name.toLowerCase().includes(searchText.toLowerCase())
    );
    setFilteredDomains(filtered);
  }, [searchText, domains]);

  useEffect(() => {
    void loadDomains();
  }, [loadDomains]);

  useEffect(() => {
    filterDomains();
  }, [filterDomains]);

  const handleCreate = () => {
    setEditingDomain(null);
    setShowForm(true);
  };

  const handleEdit = (domain: Domain) => {
    setEditingDomain(domain);
    setShowForm(true);
  };

  const handleView = async (id: number) => {
    try {
      const domain = await domainAPI.getById(id);
      setViewingDomain(domain);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 domain details'));
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await domainAPI.delete(id);
      message.success('Domain 删除成功');
      await loadDomains();
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '删除失败 domain'));
    }
  };

  const handleBatchDelete = async () => {
    if (selectedRowKeys.length === 0) {
      message.warning('请至少选择一个域名');
      return;
    }

    let successCount = 0;
    let failCount = 0;
    const errors: string[] = [];

    for (const key of selectedRowKeys) {
      try {
        await domainAPI.delete(Number(key));
        successCount++;
      } catch (err: unknown) {
        failCount++;
        errors.push(`ID ${key}: ${getApiErrorMessage(err, '删除失败')}`);
      }
    }

    if (successCount > 0) {
      message.success(`成功删除 ${successCount} 个域名`);
    }
    if (failCount > 0) {
      message.error(`删除失败 ${failCount} 个域名：${errors.join('; ')}`);
    }

    setSelectedRowKeys([]);
    await loadDomains();
  };

  const handleFormSubmit = async () => {
    setShowForm(false);
    setEditingDomain(null);
    await loadDomains();
  };

  const handleExport = () => {
    const csvContent = [
      ['编号', '名称', '创建时间', '更新时间'].join(','),
      ...(filteredDomains || []).map((d) =>
        [
          d.id,
          `"${d.name}"`,
          new Date(d.created_at).toISOString(),
          new Date(d.updated_at).toISOString(),
        ].join(',')
      ),
    ].join('\n');

    // Add UTF-8 BOM to fix Chinese character encoding issues in Excel
    const blob = new Blob(['\uFEFF' + csvContent], { type: 'text/csv;charset=utf-8;' });
    const link = document.createElement('a');
    const url = URL.createObjectURL(blob);
    link.setAttribute('href', url);
    link.setAttribute('download', `domains-${new Date().toISOString().split('T')[0]}.csv`);
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

  const columns: ColumnsType<Domain> = [
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
            title="删除域名"
            description={`确定要删除 "${record.name}"?`}
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
      total: (domains || []).length,
      filtered: (filteredDomains || []).length,
    };
  }, [domains, filteredDomains]);

  return (
    <div className="vm-page">
      <Row gutter={16} className="vm-stat-row" style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic title="域名总数" value={stats.total} />
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
        <h2 className="vm-page-title">Domains</h2>
        <Space wrap>
          <Search
            placeholder="搜索 name"
            allowClear
            style={{ width: 250 }}
            prefix={<SearchOutlined />}
            onSearch={setSearchText}
            onChange={(e) => setSearchText(e.target.value)}
          />
          <Button
            icon={<ReloadOutlined />}
            onClick={loadDomains}
            loading={loading}
          >
            刷新
          </Button>
          <Button
            icon={<ExportOutlined />}
            onClick={handleExport}
            disabled={(filteredDomains || []).length === 0}
          >
            导出
          </Button>
          {selectedRowKeys.length > 0 && (
            <Popconfirm
              title={`删除 ${selectedRowKeys.length} 域名?`}
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
            创建 Domain
          </Button>
        </Space>
      </div>

      <Table
        columns={columns}
        dataSource={filteredDomains || []}
        rowKey="id"
        loading={loading}
        rowSelection={rowSelection}
        pagination={{
          current: pagination.current,
          pageSize: pagination.pageSize,
          showSizeChanger: true,
          showQuickJumper: true,
          showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条，共 ${total} 个域名`,
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
        <DomainForm
          domain={editingDomain}
          domains={domains}
          open={showForm}
          onClose={() => {
            setShowForm(false);
            setEditingDomain(null);
          }}
          onSubmit={handleFormSubmit}
        />
      )}

      <Modal
        title="域名详情"
        open={viewingDomain !== null}
        onCancel={() => setViewingDomain(null)}
        footer={[
          <Button key="close" onClick={() => setViewingDomain(null)}>
            关闭
          </Button>,
          <Button
            key="edit"
            type="primary"
            onClick={() => {
              if (viewingDomain) {
                setViewingDomain(null);
                handleEdit(viewingDomain);
              }
            }}
          >
            编辑
          </Button>,
        ]}
      >
        {viewingDomain && (
          <Descriptions column={1} bordered>
            <Descriptions.Item label="ID">{viewingDomain.id}</Descriptions.Item>
            <Descriptions.Item label="名称">{viewingDomain.name}</Descriptions.Item>
            <Descriptions.Item label="创建时间">
              {new Date(viewingDomain.created_at).toLocaleString()}
            </Descriptions.Item>
            <Descriptions.Item label="更新时间">
              {new Date(viewingDomain.updated_at).toLocaleString()}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
}

