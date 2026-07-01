// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect, useMemo } from 'react';
import { Card, Statistic, Row, Col, Table, Spin, message, Switch, Space, Typography, Tag, Progress, Select, Button } from 'antd';
import {
  CloudServerOutlined,
  LinkOutlined,
  GlobalOutlined,
  PlayCircleOutlined,
  ApiOutlined,
  ReloadOutlined,
  ClockCircleOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { statsAPI } from '../lib/api';
import { getApiErrorMessage } from '../lib/httpError';

const { Text } = Typography;

interface Stats {
  providers: number;
  lines: number;
  domains: number;
  streams: number;
  stream_paths: number;
  endpoints: number;
  endpoints_enabled: number;
  endpoints_disabled: number;
  lines_by_provider: Array<{
    provider_id: number;
    provider_name: string;
    line_count: number;
  }>;
  endpoints_by_stream: Array<{
    stream_id: number;
    stream_name: string;
    stream_code: string;
    endpoint_count: number;
  }>;
  endpoints_by_domain: Array<{
    domain_id: number;
    domain_name: string;
    endpoint_count: number;
  }>;
}

export default function DashboardPage() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [refreshInterval, setRefreshInterval] = useState(30); // seconds
  const [lastUpdatedAt, setLastUpdatedAt] = useState<Date | null>(null);

  useEffect(() => {
    loadStats();
  }, []);

  useEffect(() => {
    if (!autoRefresh) return;

    const interval = setInterval(() => {
      loadStats();
    }, refreshInterval * 1000);

    return () => clearInterval(interval);
  }, [autoRefresh, refreshInterval]);

  const loadStats = async () => {
    try {
      setLoading(true);
      const data = await statsAPI.getStats();
      setStats(data);
      setLastUpdatedAt(new Date());
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载统计数据失败'));
    } finally {
      setLoading(false);
    }
  };

  const providerColumns: ColumnsType<Stats['lines_by_provider'][0]> = [
    {
      title: '厂商名称',
      dataIndex: 'provider_name',
      key: 'provider_name',
      render: (name: string) => <Text strong>{name}</Text>,
    },
    {
      title: '线路数量',
      dataIndex: 'line_count',
      key: 'line_count',
      sorter: (a, b) => a.line_count - b.line_count,
      render: (count: number) => {
        const total = stats?.lines || 1;
        const percentage = Math.round((count / total) * 100);
        return (
          <Space size={8}>
            <Tag color="blue">{count}</Tag>
            <Text type="secondary">{percentage}%</Text>
          </Space>
        );
      },
    },
  ];

  const streamColumns: ColumnsType<Stats['endpoints_by_stream'][0]> = [
    {
      title: '视频流区域',
      key: 'stream',
      render: (_, record) => (
        <Text strong>{record.stream_name}</Text>
      ),
    },
    {
      title: '端点数量',
      dataIndex: 'endpoint_count',
      key: 'endpoint_count',
      sorter: (a, b) => a.endpoint_count - b.endpoint_count,
      render: (count: number) => {
        const total = stats?.endpoints || 1;
        const percentage = Math.round((count / total) * 100);
        return (
          <Space size={8}>
            <Tag color="green">{count}</Tag>
            <Text type="secondary">{percentage}%</Text>
          </Space>
        );
      },
    },
  ];

  const domainColumns: ColumnsType<Stats['endpoints_by_domain'][0]> = [
    {
      title: '域名',
      dataIndex: 'domain_name',
      key: 'domain_name',
      render: (name: string) => <Text strong>{name}</Text>,
    },
    {
      title: '端点数量',
      dataIndex: 'endpoint_count',
      key: 'endpoint_count',
      sorter: (a, b) => a.endpoint_count - b.endpoint_count,
      render: (count: number) => <Tag color="cyan">{count}</Tag>,
    },
  ];

  const enabledPercentage = useMemo(() => {
    if (!stats || stats.endpoints === 0) return 0;
    return Math.round((stats.endpoints_enabled / stats.endpoints) * 100);
  }, [stats]);

  const coreMetrics = useMemo(
    () => [
      { title: 'CDN 厂商', value: stats?.providers || 0, icon: <CloudServerOutlined />, strip: 'vm-dash-strip-teal' },
      { title: 'CDN 线路', value: stats?.lines || 0, icon: <LinkOutlined />, strip: 'vm-dash-strip-sky' },
      { title: '域名', value: stats?.domains || 0, icon: <GlobalOutlined />, strip: 'vm-dash-strip-violet' },
      { title: '端点总数', value: stats?.endpoints || 0, icon: <ApiOutlined />, strip: 'vm-dash-strip-slate' },
    ],
    [stats]
  );

  if (loading && !stats) {
    return (
      <div style={{ textAlign: 'center', padding: '100px' }}>
        <Spin size="large" />
        <div style={{ marginTop: 16 }}>
          <Text type="secondary">正在加载仪表板数据...</Text>
        </div>
      </div>
    );
  }

  return (
    <div className="vm-page">
      <div
        style={{
          marginBottom: 24,
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'flex-start',
          flexWrap: 'wrap',
          gap: 16,
        }}
      >
        <div className="vm-page-head">
          <h1 className="vm-page-title">仪表盘</h1>
          <p className="vm-page-desc">核心资源与端点分布总览</p>
        </div>
        <Space wrap size={[8, 8]}>
          <Space>
            <Text type="secondary">自动刷新</Text>
            <Switch checked={autoRefresh} onChange={setAutoRefresh} />
          </Space>
          <Select
            value={refreshInterval}
            disabled={!autoRefresh}
            style={{ width: 112 }}
            onChange={(v) => setRefreshInterval(v)}
            options={[
              { value: 10, label: '10 秒' },
              { value: 30, label: '30 秒' },
              { value: 60, label: '60 秒' },
              { value: 120, label: '2 分钟' },
            ]}
          />
          <Space>
            <ClockCircleOutlined style={{ color: autoRefresh ? '#059669' : '#cbd5e1' }} />
            <Text type="secondary" style={{ fontSize: 12 }}>
              {autoRefresh ? `每 ${refreshInterval} 秒刷新` : '已关闭'}
            </Text>
          </Space>
          <Text type="secondary" style={{ fontSize: 12 }}>
            {lastUpdatedAt ? `更新于 ${lastUpdatedAt.toLocaleTimeString()}` : '尚未更新'}
          </Text>
          <Button icon={<ReloadOutlined spin={loading} />} onClick={() => loadStats()}>
            刷新
          </Button>
        </Space>
      </div>

      <Row gutter={[16, 16]} className="vm-stat-row" style={{ marginBottom: 24 }}>
        {coreMetrics.map((item) => (
          <Col key={item.title} xs={24} sm={12} lg={6}>
            <Card hoverable className={`vm-dash-stat ${item.strip}`} styles={{ body: { padding: '20px' } }}>
              <Statistic
                title={item.title}
                value={item.value}
                prefix={<span className="vm-dash-stat-icon">{item.icon}</span>}
                styles={{ content: { fontSize: 28, fontWeight: 700 } }}
              />
            </Card>
          </Col>
        ))}
        <Col xs={24} sm={12} lg={12}>
          <Card hoverable className="vm-panel-card" styles={{ body: { padding: '18px 20px' } }}>
            <Space direction="vertical" size={10} style={{ width: '100%' }}>
              <Space style={{ justifyContent: 'space-between', width: '100%' }}>
                <Text strong>端点健康度</Text>
                <Text type="secondary">{enabledPercentage}% 启用</Text>
              </Space>
              <Progress percent={enabledPercentage} strokeColor="#0d9488" showInfo={false} />
              <Space size={18}>
                <Tag color="green">启用 {stats?.endpoints_enabled || 0}</Tag>
                <Tag color="red">禁用 {stats?.endpoints_disabled || 0}</Tag>
                <Tag color="blue">视频流区域 {stats?.streams || 0}</Tag>
              </Space>
            </Space>
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} style={{ display: 'flex', alignItems: 'stretch' }}>
        <Col xs={24} lg={12} style={{ display: 'flex' }}>
          <Card
            className="vm-panel-card"
            title={
              <Space>
                <CloudServerOutlined style={{ color: '#0d9488' }} />
                <Text strong>按厂商统计线路</Text>
              </Space>
            }
            style={{
              width: '100%',
              display: 'flex',
              flexDirection: 'column',
            }}
            styles={{ body: { flex: 1, display: 'flex', flexDirection: 'column', minHeight: '400px' } }}
          >
            <Table
              columns={providerColumns}
              dataSource={stats?.lines_by_provider || []}
              rowKey="provider_id"
              pagination={false}
              size="small"
              loading={loading}
              style={{ flex: 1 }}
            />
          </Card>
        </Col>
        <Col xs={24} lg={12} style={{ display: 'flex' }}>
          <Card
            className="vm-panel-card"
            title={
              <Space>
                <PlayCircleOutlined style={{ color: '#059669' }} />
                <Text strong>按视频流区域统计端点</Text>
              </Space>
            }
            style={{
              width: '100%',
              display: 'flex',
              flexDirection: 'column',
            }}
            styles={{ body: { flex: 1, display: 'flex', flexDirection: 'column', minHeight: '400px' } }}
          >
            <Table
              columns={streamColumns}
              dataSource={stats?.endpoints_by_stream || []}
              rowKey="stream_id"
              pagination={false}
              size="small"
              loading={loading}
              style={{ flex: 1 }}
            />
          </Card>
        </Col>
        <Col xs={24}>
          <Card
            className="vm-panel-card"
            title={
              <Space>
                <GlobalOutlined style={{ color: '#0284c7' }} />
                <Text strong>按域名统计端点</Text>
              </Space>
            }
          >
            <Table
              columns={domainColumns}
              dataSource={stats?.endpoints_by_domain || []}
              rowKey="domain_id"
              pagination={false}
              size="small"
              loading={loading}
            />
          </Card>
        </Col>
      </Row>
    </div>
  );
}
