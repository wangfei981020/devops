// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import { Button, Table, Space, Select, message, Input, Card, Statistic, Row, Col, Modal, Descriptions, Tag, Switch } from 'antd';
import { SearchOutlined, ReloadOutlined, EyeOutlined, ExportOutlined, PlayCircleOutlined, ExperimentOutlined, CopyOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { videoStreamEndpointAPI, lineAPI, domainAPI, streamAPI, providerAPI } from '../lib/api';
import type { VideoStreamEndpoint, CDNLine, Domain, Stream, CDNProvider } from '../lib/api';
import { selectSearchableProps } from '../lib/selectSearchProps';
import { displayStreamSeries } from '../lib/streamSeries';
import { getApiErrorMessage } from '../lib/httpError';
import { auth } from '../lib/auth';
import flvjs from 'flv.js';

const { Search } = Input;

/** 端点嵌套的 stream_path 推导系列展示文案 */
function endpointSeriesLabel(e: VideoStreamEndpoint): string {
  const sp = e.stream_path;
  if (!sp) return '—';
  return displayStreamSeries(sp);
}

export default function VideoStreamEndpointsPage() {
  const currentUser = auth.getUser();
  const isAdmin = Boolean(currentUser?.is_admin);
  const [endpoints, setEndpoints] = useState<VideoStreamEndpoint[]>([]);
  const [filteredEndpoints, setFilteredEndpoints] = useState<VideoStreamEndpoint[]>([]);
  const [lines, setLines] = useState<CDNLine[]>([]);
  const [domains, setDomains] = useState<Domain[]>([]);
  const [streams, setStreams] = useState<Stream[]>([]);
  const [providers, setProviders] = useState<CDNProvider[]>([]);
  const [loading, setLoading] = useState(true);
  const [viewingEndpoint, setViewingEndpoint] = useState<VideoStreamEndpoint | null>(null);
  const [playingEndpoint, setPlayingEndpoint] = useState<VideoStreamEndpoint | null>(null);
  const [playModalVisible, setPlayModalVisible] = useState(false);
  const videoRef = useRef<HTMLVideoElement>(null);
  const flvPlayerRef = useRef<flvjs.Player | null>(null);
  const errorShownRef = useRef<boolean>(false);
  const retryCountRef = useRef<number>(0);
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const playingEndpointRef = useRef<VideoStreamEndpoint | null>(null);
  const initialLoadRef = useRef<boolean>(false); // 跟踪是否已完成初始加载
  const MAX_RETRIES = 3; // 最大重试次数
  const RETRY_DELAY = 2000; // 重试延迟（毫秒）
  const [filterLineId, setFilterLineId] = useState<number | undefined>(undefined);
  const [filterDomainId, setFilterDomainId] = useState<number | undefined>(undefined);
  const [filterStreamId, setFilterStreamId] = useState<number | undefined>(undefined);
  const [filterProviderId, setFilterProviderId] = useState<number | undefined>(undefined);
  const [filterStatus, setFilterStatus] = useState<number | undefined>(undefined);
  const [filterTableId, setFilterTableId] = useState<string | undefined>(undefined);
  const [filterSeries, setFilterSeries] = useState<string | undefined>(undefined);
  const [filterResolution, setFilterResolution] = useState<string | undefined>(undefined);
  const [testingResolution, setTestingResolution] = useState<Set<number>>(new Set());
  const [searchText, setSearchText] = useState('');
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10 });

  const fetchBaseResources = useCallback(async () => {
    const [linesData, domainsData, streamsData, providersData] = await Promise.all([
      lineAPI.getAll(),
      domainAPI.getAll(),
      streamAPI.getAll(),
      providerAPI.getAll(),
    ]);
    setLines(linesData || []);
    setDomains(domainsData || []);
    setStreams(streamsData || []);
    setProviders(providersData || []);
  }, []);

  const loadEndpoints = useCallback(async () => {
    try {
      if (!initialLoadRef.current) {
        setLoading(true);
      }
      const filters: Record<string, number | string> = {};
      if (filterLineId) filters.line_id = filterLineId;
      if (filterDomainId) filters.domain_id = filterDomainId;
      if (filterStreamId) filters.stream_id = filterStreamId;
      if (filterProviderId) filters.provider_id = filterProviderId;
      if (filterStatus !== undefined) filters.status = filterStatus;
      if (filterResolution) filters.resolution = filterResolution;

      const data = await videoStreamEndpointAPI.getAll(Object.keys(filters).length > 0 ? filters : undefined);
      if (data && Array.isArray(data)) {
        setEndpoints(data);
      } else {
        setEndpoints([]);
      }
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 endpoints'));
      setEndpoints([]);
    } finally {
      if (!initialLoadRef.current) {
        setLoading(false);
      }
    }
  }, [
    filterLineId,
    filterDomainId,
    filterStreamId,
    filterProviderId,
    filterStatus,
    filterResolution,
  ]);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        setLoading(true);
        initialLoadRef.current = false;
        await fetchBaseResources();
        if (cancelled) return;
        await loadEndpoints();
        if (cancelled) return;
        initialLoadRef.current = true;
      } catch (err: unknown) {
        message.error(getApiErrorMessage(err, '加载失败 data'));
        setLines([]);
        setDomains([]);
        setStreams([]);
        setProviders([]);
        try {
          await loadEndpoints();
          initialLoadRef.current = true;
        } catch (endpointErr: unknown) {
          console.error('Failed to load endpoints:', endpointErr);
          initialLoadRef.current = true;
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentional one-time bootstrap; loadEndpoints/filters captured at mount
  }, []);

  useEffect(() => {
    if (initialLoadRef.current) {
      void loadEndpoints();
    }
  }, [
    filterLineId,
    filterDomainId,
    filterStreamId,
    filterProviderId,
    filterStatus,
    filterResolution,
    loadEndpoints,
  ]);

  const filterEndpoints = useCallback(() => {
    const endpointsList = endpoints || [];
    let filtered = endpointsList;

    if (filterSeries) {
      filtered = filtered.filter((endpoint) => endpointSeriesLabel(endpoint) === filterSeries);
    }

    if (searchText.trim()) {
      const q = searchText.toLowerCase();
      filtered = filtered.filter((endpoint) => {
        const seriesText = endpointSeriesLabel(endpoint);
        return (
          endpoint.full_url.toLowerCase().includes(q) ||
          endpoint.provider?.name.toLowerCase().includes(q) ||
          endpoint.provider?.code.toLowerCase().includes(q) ||
          endpoint.line?.name.toLowerCase().includes(q) ||
          endpoint.domain?.name.toLowerCase().includes(q) ||
          endpoint.stream?.name.toLowerCase().includes(q) ||
          endpoint.stream_path?.table_id?.toLowerCase().includes(q) ||
          seriesText.toLowerCase().includes(q)
        );
      });
    }

    if (filterTableId) {
      filtered = filtered.filter((endpoint) => endpoint.stream_path?.table_id === filterTableId);
    }

    setFilteredEndpoints(filtered);
  }, [searchText, endpoints, filterTableId, filterSeries]);

  useEffect(() => {
    filterEndpoints();
  }, [filterEndpoints]);

  const handleRefresh = async () => {
    try {
      setLoading(true);
      // 所有登录用户统一走重生成+刷新，确保与管理员看到一致的最新数据
      const result = await videoStreamEndpointAPI.generateAll();
      message.success(`已重新生成 ${result.count} 个端点`);
      await loadEndpoints();
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '刷新端点失败'));
      await loadEndpoints();
    } finally {
      setLoading(false);
    }
  };

  const handleView = useCallback(async (id: number) => {
    try {
      const endpoint = await videoStreamEndpointAPI.getById(id);
      setViewingEndpoint(endpoint);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 endpoint details'));
    }
  }, []);

  const handlePlay = useCallback(async (id: number) => {
    try {
      const endpoint = await videoStreamEndpointAPI.getById(id);
      if (!endpoint.full_url) {
        message.error('该端点没有有效的 URL');
        return;
      }
      setPlayingEndpoint(endpoint);
      setPlayModalVisible(true);
    } catch (err: unknown) {
      message.error(getApiErrorMessage(err, '加载失败 endpoint details'));
    }
  }, []);

  const handleClosePlayModal = () => {
    setPlayModalVisible(false);
    // 清理重试定时器
    if (retryTimerRef.current) {
      clearTimeout(retryTimerRef.current);
      retryTimerRef.current = null;
    }
    // 清理 FLV 播放器
    if (flvPlayerRef.current) {
      flvPlayerRef.current.pause();
      flvPlayerRef.current.unload();
      flvPlayerRef.current.detachMediaElement();
      flvPlayerRef.current.destroy();
      flvPlayerRef.current = null;
    }
    // 重置错误显示标志和重试计数
    errorShownRef.current = false;
    retryCountRef.current = 0;
    playingEndpointRef.current = null;
    setPlayingEndpoint(null);
  };

  // 初始化 FLV 播放器的函数
  const initializePlayer = useCallback(() => {
    // 使用 ref 获取最新的 playingEndpoint，避免闭包问题
    const currentEndpoint = playingEndpointRef.current;
    if (!videoRef.current || !flvjs.isSupported() || !currentEndpoint) {
      if (!flvjs.isSupported()) {
        console.warn('FLV.js is not supported in this browser');
      }
      return;
    }

    const videoElement = videoRef.current;

    // 清理之前的播放器
    if (flvPlayerRef.current) {
      try {
        flvPlayerRef.current.pause();
        flvPlayerRef.current.unload();
        flvPlayerRef.current.detachMediaElement();
        flvPlayerRef.current.destroy();
      } catch (e) {
        console.error('Error destroying previous player:', e);
      }
      flvPlayerRef.current = null;
    }

    try {
      const player = flvjs.createPlayer({
        type: 'flv',
        url: currentEndpoint.full_url,
        isLive: true,
      }, {
        enableWorker: false,
        enableStashBuffer: false,
        stashInitialSize: 128,
        autoCleanupSourceBuffer: true,
      });

      player.attachMediaElement(videoElement);
      player.load();

      // 尝试自动播放
      const playPromise = player.play();
      if (playPromise !== undefined && playPromise instanceof Promise) {
        playPromise.catch((err: unknown) => {
          console.error('Play error:', err);
          message.warning('自动播放失败，请手动点击播放按钮');
        });
      }

      flvPlayerRef.current = player;

      // 错误处理 - 自动重试机制
      player.on(flvjs.Events.ERROR, (errorType: unknown, errorDetail: unknown, errorInfo: unknown) => {
        console.error('FLV Player Error:', errorType, errorDetail, errorInfo);
        
        // 如果已经显示过错误，不再重复显示
        if (errorShownRef.current) {
          return;
        }
        
        errorShownRef.current = true;
        let errorMsg = '播放失败';
        if (errorType === flvjs.ErrorTypes.NETWORK_ERROR) {
          errorMsg = '网络错误，请检查流地址是否可访问';
        } else if (errorType === flvjs.ErrorTypes.MEDIA_ERROR) {
          errorMsg = '媒体格式错误，请检查流格式是否正确';
        }

        // 检查是否可以重试
        if (retryCountRef.current < MAX_RETRIES) {
          retryCountRef.current += 1;
          message.warning(`${errorMsg}，正在重试 (${retryCountRef.current}/${MAX_RETRIES})...`);
          
          // 清理当前播放器
          if (flvPlayerRef.current) {
            try {
              flvPlayerRef.current.pause();
              flvPlayerRef.current.unload();
              flvPlayerRef.current.detachMediaElement();
              flvPlayerRef.current.destroy();
            } catch (e) {
              console.error('Error destroying player before retry:', e);
            }
            flvPlayerRef.current = null;
          }

          // 延迟后重试
          retryTimerRef.current = setTimeout(() => {
            errorShownRef.current = false; // 重置错误标志，允许重试时显示错误
            initializePlayer();
          }, RETRY_DELAY);
        } else {
          // 达到最大重试次数，显示最终错误
          message.error(`${errorMsg}，已重试 ${MAX_RETRIES} 次，请检查流地址或稍后再试`);
        }
      });

      // 监听加载完成 - 重置重试计数
      player.on(flvjs.Events.LOADING_COMPLETE, () => {
        console.log('FLV stream loaded');
        // 加载成功，重置重试计数和错误标志
        retryCountRef.current = 0;
        errorShownRef.current = false;
      });
    } catch (err: unknown) {
      console.error('Error creating FLV player:', err);
      message.error('创建播放器失败：' + (err instanceof Error ? err.message : String(err)));
    }
  }, []);

  // 初始化 FLV 播放器
  useEffect(() => {
    if (!playModalVisible || !playingEndpoint) {
      return;
    }

    // 更新 playingEndpoint ref
    playingEndpointRef.current = playingEndpoint;
    
    // 重置重试计数和错误标志
    retryCountRef.current = 0;
    errorShownRef.current = false;

    // 等待 Modal 完全打开后再初始化播放器
    const timer = setTimeout(() => {
      initializePlayer();
    }, 300); // 延迟 300ms 确保 DOM 已渲染

    return () => {
      clearTimeout(timer);
      // 清理重试定时器
      if (retryTimerRef.current) {
        clearTimeout(retryTimerRef.current);
        retryTimerRef.current = null;
      }
      if (flvPlayerRef.current) {
        try {
          flvPlayerRef.current.pause();
          flvPlayerRef.current.unload();
          flvPlayerRef.current.detachMediaElement();
          flvPlayerRef.current.destroy();
        } catch (e) {
          console.error('Error cleaning up player:', e);
        }
        flvPlayerRef.current = null;
      }
      // 重置错误显示标志和重试计数
      errorShownRef.current = false;
      retryCountRef.current = 0;
    };
  }, [playModalVisible, playingEndpoint, initializePlayer]);


  const handleToggleStatus = useCallback(
    async (id: number, currentStatus: number) => {
      try {
        const newStatus = currentStatus === 1 ? 0 : 1;
        await videoStreamEndpointAPI.updateStatus(id, newStatus);
        message.success(`Endpoint ${newStatus === 1 ? 'enabled' : 'disabled'} successfully`);
        await loadEndpoints();
      } catch (err: unknown) {
        message.error(getApiErrorMessage(err, 'Failed to update status'));
      }
    },
    [loadEndpoints]
  );

  const handleTestResolution = useCallback(
    async (id: number) => {
      try {
        setTestingResolution((prev) => new Set(prev).add(id));
        const result = await videoStreamEndpointAPI.testResolution(id);
        message.success(`分辨率检测成功：已更新为 ${result.resolution}`);
        await loadEndpoints();
      } catch (err: unknown) {
        message.error(getApiErrorMessage(err, '分辨率检测失败'));
      } finally {
        setTestingResolution((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
      }
    },
    [loadEndpoints]
  );

  const handleCopyUrl = useCallback(async (url: string) => {
    try {
      await navigator.clipboard.writeText(url);
      message.success('URL 已复制到剪贴板');
    } catch {
      const textArea = document.createElement('textarea');
      textArea.value = url;
      textArea.style.position = 'fixed';
      textArea.style.opacity = '0';
      document.body.appendChild(textArea);
      textArea.select();
      try {
        document.execCommand('copy');
        message.success('URL 已复制到剪贴板');
      } catch {
        message.error('复制失败，请手动复制');
      }
      document.body.removeChild(textArea);
    }
  }, []);


  const handleExport = () => {
    const csvContent = [
      ['编号', '完整URL', '厂商', '线路', '域名', '流区域', '桌台号', '系列', '路径', '分辨率', '状态', '创建时间'].join(','),
      ...(filteredEndpoints || []).map((e) =>
        [
          e.id,
          `"${e.full_url}"`,
          `"${e.provider?.name || 'Unknown'}"`,
          `"${e.line?.name || 'Unknown'}"`,
          `"${e.domain?.name || 'Unknown'}"`,
          `"${e.stream?.name || 'Unknown'}"`,
          `"${e.stream_path?.table_id || 'N/A'}"`,
          `"${endpointSeriesLabel(e)}"`,
          `"${e.stream_path?.full_path || 'Unknown'}"`,
          `"${e.resolution || '普清'}"`,
          e.status === 1 ? '已启用' : '已禁用',
          new Date(e.created_at).toISOString(),
        ].join(',')
      ),
    ].join('\n');

    // Add UTF-8 BOM to fix Chinese character encoding issues in Excel
    const blob = new Blob(['\uFEFF' + csvContent], { type: 'text/csv;charset=utf-8;' });
    const link = document.createElement('a');
    const url = URL.createObjectURL(blob);
    link.setAttribute('href', url);
    link.setAttribute('download', `video-stream-endpoints-${new Date().toISOString().split('T')[0]}.csv`);
    link.style.visibility = 'hidden';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    message.success('Data 导出成功');
  };

  // Calculate unique table IDs for filters
  const tableIdFilters = useMemo(() => {
    const tableIds = new Set<string>();
    (endpoints || []).forEach((e) => {
      if (e.stream_path?.table_id) {
        tableIds.add(e.stream_path.table_id);
      }
    });
    return Array.from(tableIds).sort().map((id) => ({ text: id, value: id }));
  }, [endpoints]);

  const seriesFilterOptions = useMemo(() => {
    const labels = new Set<string>();
    (endpoints || []).forEach((e) => {
      labels.add(endpointSeriesLabel(e));
    });
    return Array.from(labels).sort((a, b) => a.localeCompare(b, 'zh-CN'));
  }, [endpoints]);

  const columns: ColumnsType<VideoStreamEndpoint> = useMemo(() => [
    {
      title: '桌台号',
      key: 'table_id',
      render: (_, record) => record.stream_path?.table_id || 'N/A',
      filters: tableIdFilters,
      onFilter: (value, record) => record.stream_path?.table_id === value,
    },
    {
      title: '系列',
      key: 'series',
      width: 100,
      render: (_, record) => endpointSeriesLabel(record),
      sorter: (a, b) =>
        endpointSeriesLabel(a).localeCompare(endpointSeriesLabel(b), 'zh-CN'),
    },
    {
      title: '完整URL',
      dataIndex: 'full_url',
      key: 'full_url',
      render: (url: string) => (
        <Space>
          <a href={url} target="_blank" rel="noopener noreferrer">
            {url}
          </a>
          <Button
            type="text"
            icon={<CopyOutlined />}
            size="small"
            onClick={() => handleCopyUrl(url)}
            title="复制URL"
          />
        </Space>
      ),
      ellipsis: true,
    },
    {
      title: '厂商',
      key: 'provider',
      render: (_, record) => record.provider?.name || 'Unknown',
      filters: (providers || []).map((p) => ({ text: p.name, value: p.id })),
      onFilter: (value, record) => record.provider_id === value,
    },
    {
      title: '线路',
      key: 'line',
      render: (_, record) => record.line?.name || 'Unknown',
      filters: (lines || []).map((l) => ({ text: l.name, value: l.id })),
      onFilter: (value, record) => record.line_id === value,
    },
    {
      title: '域名',
      key: 'domain',
      render: (_, record) => record.domain?.name || 'Unknown',
      filters: (domains || []).map((d) => ({ text: d.name, value: d.id })),
      onFilter: (value, record) => record.domain_id === value,
    },
    {
      title: '视频流区域',
      key: 'stream',
      render: (_, record) => record.stream?.name || 'Unknown',
      filters: (streams || []).map((s) => ({ text: s.name, value: s.id })),
      onFilter: (value, record) => record.stream_id === value,
    },
    {
      title: '路径',
      key: 'path',
      render: (_, record) => record.stream_path?.full_path || 'Unknown',
    },
    {
      title: '分辨率',
      dataIndex: 'resolution',
      key: 'resolution',
      width: 100,
      render: (resolution: string) => {
        if (!resolution) return <Tag>普清</Tag>;
        const color = resolution === '超清' ? 'purple' : resolution === '高清' ? 'blue' : 'default';
        return <Tag color={color}>{resolution}</Tag>;
      },
      filters: [
        { text: '普清', value: '普清' },
        { text: '高清', value: '高清' },
        { text: '超清', value: '超清' },
      ],
      onFilter: (value, record) => (record.resolution || '普清') === value,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      className: 'vm-endpoints-status-col',
      width: 100,
      render: (status: number) => (
        <Tag color={status === 1 ? 'green' : 'red'}>
          {status === 1 ? '已启用' : '已禁用'}
        </Tag>
      ),
      filters: [
        { text: '已启用', value: 1 },
        { text: '已禁用', value: 0 },
      ],
      onFilter: (value, record) => record.status === value,
    },
    {
      title: '操作',
      key: 'actions',
      className: 'vm-endpoints-action-col',
      width: 220,
      fixed: 'right' as const,
      render: (_, record) => (
        <Space size="small" className="vm-endpoints-action-inner">
          <Button
            type="link"
            icon={<EyeOutlined />}
            onClick={() => handleView(record.id)}
            size="small"
          >
            查看
          </Button>
          <Button
            type="link"
            icon={<PlayCircleOutlined />}
            onClick={() => handlePlay(record.id)}
            disabled={record.status === 0}
            size="small"
          >
            播放
          </Button>
          <Button
            type="link"
            icon={<ExperimentOutlined />}
            onClick={() => handleTestResolution(record.id)}
            loading={testingResolution.has(record.id)}
            disabled={!isAdmin || testingResolution.has(record.id)}
            size="small"
          >
            分辨率检测
          </Button>
          <Switch
            checked={record.status === 1}
            onChange={() => handleToggleStatus(record.id, record.status)}
            disabled={!isAdmin}
            size="small"
          />
        </Space>
      ),
    },
  ], [
    lines,
    domains,
    streams,
    providers,
    tableIdFilters,
    handleView,
    handlePlay,
    handleTestResolution,
    testingResolution,
    handleToggleStatus,
    handleCopyUrl,
    isAdmin,
  ]);

  const stats = useMemo(() => {
    const endpointsList = endpoints || [];
    const filteredList = filteredEndpoints || [];
    return {
      total: endpointsList.length,
      filtered: filteredList.length,
      enabled: endpointsList.filter((e) => e.status === 1).length,
      disabled: endpointsList.filter((e) => e.status === 0).length,
    };
  }, [endpoints, filteredEndpoints]);

  return (
    <div className="vm-page">
      <Row gutter={16} className="vm-stat-row" style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic title="端点总数" value={stats.total} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="筛选结果" value={stats.filtered} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="已启用" value={stats.enabled} styles={{ content: { color: '#3f8600' } }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="已禁用" value={stats.disabled} styles={{ content: { color: '#cf1322' } }} />
          </Card>
        </Col>
      </Row>

      <div
        className="vm-toolbar-panel"
        style={{ marginBottom: 16, width: '100%', maxWidth: '100%', boxSizing: 'border-box' }}
      >
        <h2 className="vm-page-title" style={{ margin: '0 0 12px' }}>
          视频流端点
        </h2>
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            gap: '8px 12px',
            alignItems: 'center',
            width: '100%',
          }}
        >
          <Search
            placeholder="搜索 URL、厂商、线路、域名、视频流区域、桌台号或系列"
            allowClear
            style={{ flex: '1 1 220px', minWidth: 160, maxWidth: 420 }}
            prefix={<SearchOutlined />}
            onSearch={setSearchText}
            onChange={(e) => setSearchText(e.target.value)}
          />
          <Select
            placeholder="筛选厂商"
            allowClear
            style={{ flex: '1 1 128px', minWidth: 112, maxWidth: 180 }}
            onChange={(value) => setFilterProviderId(value)}
            value={filterProviderId}
            {...selectSearchableProps}
          >
            {(providers || []).map((provider) => (
              <Select.Option
                key={provider.id}
                value={provider.id}
                label={`${provider.name} ${provider.code}`}
              >
                {provider.name} ({provider.code})
              </Select.Option>
            ))}
          </Select>
          <Select
            placeholder="筛选厂商线路"
            allowClear
            style={{ flex: '1 1 128px', minWidth: 112, maxWidth: 180 }}
            onChange={(value) => setFilterLineId(value)}
            value={filterLineId}
            {...selectSearchableProps}
          >
            {(lines || []).map((line) => (
              <Select.Option
                key={line.id}
                value={line.id}
                label={`${line.name} ${line.code}`}
              >
                {line.name} ({line.code})
              </Select.Option>
            ))}
          </Select>
          <Select
            placeholder="筛选 Domain"
            allowClear
            style={{ flex: '1 1 128px', minWidth: 112, maxWidth: 180 }}
            onChange={(value) => setFilterDomainId(value)}
            value={filterDomainId}
            {...selectSearchableProps}
          >
            {(domains || []).map((domain) => (
              <Select.Option key={domain.id} value={domain.id} label={domain.name}>
                {domain.name}
              </Select.Option>
            ))}
          </Select>
          <Select
            placeholder="筛选视频流区域"
            allowClear
            style={{ flex: '1 1 128px', minWidth: 112, maxWidth: 200 }}
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
          <Select
            placeholder="筛选 状态"
            allowClear
            style={{ flex: '0 1 108px', minWidth: 100, maxWidth: 140 }}
            onChange={(value) => setFilterStatus(value)}
            value={filterStatus}
            {...selectSearchableProps}
          >
            <Select.Option value={1} label="已启用">
              已启用
            </Select.Option>
            <Select.Option value={0} label="已禁用">
              已禁用
            </Select.Option>
          </Select>
          <Select
            placeholder="筛选桌台号"
            allowClear
            style={{ flex: '1 1 128px', minWidth: 112, maxWidth: 180 }}
            onChange={(value) => setFilterTableId(value)}
            value={filterTableId}
            {...selectSearchableProps}
          >
            {tableIdFilters.map((filter) => (
              <Select.Option key={filter.value} value={filter.value} label={filter.text}>
                {filter.text}
              </Select.Option>
            ))}
          </Select>
          <Select
            placeholder="筛选系列"
            allowClear
            style={{ flex: '1 1 128px', minWidth: 112, maxWidth: 180 }}
            onChange={(value) => setFilterSeries(value)}
            value={filterSeries}
            {...selectSearchableProps}
          >
            {seriesFilterOptions.map((s) => (
              <Select.Option key={s} value={s} label={s}>
                {s}
              </Select.Option>
            ))}
          </Select>
          <Select
            placeholder="筛选分辨率"
            allowClear
            style={{ flex: '0 1 108px', minWidth: 100, maxWidth: 140 }}
            onChange={(value) => setFilterResolution(value)}
            value={filterResolution}
            {...selectSearchableProps}
          >
            <Select.Option value="普清" label="普清">
              普清
            </Select.Option>
            <Select.Option value="高清" label="高清">
              高清
            </Select.Option>
            <Select.Option value="超清" label="超清">
              超清
            </Select.Option>
          </Select>
          <Button
            icon={<ReloadOutlined />}
            onClick={handleRefresh}
            loading={loading}
          >
            刷新
          </Button>
          <Button
            icon={<ExportOutlined />}
            onClick={handleExport}
            disabled={(filteredEndpoints || []).length === 0}
          >
            导出
          </Button>
        </div>
      </div>

      <Table
        columns={columns}
        dataSource={filteredEndpoints || []}
        rowKey="id"
        loading={loading}
        pagination={{
          current: pagination.current,
          pageSize: pagination.pageSize,
          showSizeChanger: true,
          showQuickJumper: true,
          showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条，共 ${total} 个端点`,
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


      <Modal
        title="端点详情"
        open={viewingEndpoint !== null}
        onCancel={() => setViewingEndpoint(null)}
        footer={[
          <Button key="close" onClick={() => setViewingEndpoint(null)}>
            关闭
          </Button>,
        ]}
        width={800}
      >
        {viewingEndpoint && (
          <Descriptions column={1} bordered>
            <Descriptions.Item label="ID">{viewingEndpoint.id}</Descriptions.Item>
            <Descriptions.Item label="完整URL">
              <Space>
                <a href={viewingEndpoint.full_url} target="_blank" rel="noopener noreferrer">
                  {viewingEndpoint.full_url}
                </a>
                <Button
                  type="text"
                  icon={<CopyOutlined />}
                  size="small"
                  onClick={() => handleCopyUrl(viewingEndpoint.full_url)}
                  title="复制URL"
                />
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="厂商">
              {viewingEndpoint.provider?.name || 'Unknown'} ({viewingEndpoint.provider?.code || 'N/A'})
            </Descriptions.Item>
            <Descriptions.Item label="Line">
              {viewingEndpoint.line?.name || 'Unknown'} ({viewingEndpoint.line?.code || 'N/A'})
            </Descriptions.Item>
            <Descriptions.Item label="域名">{viewingEndpoint.domain?.name || 'Unknown'}</Descriptions.Item>
            <Descriptions.Item label="视频流区域">
              {viewingEndpoint.stream?.name || 'Unknown'} ({viewingEndpoint.stream?.code || 'N/A'})
            </Descriptions.Item>
            <Descriptions.Item label="桌台号">
              {viewingEndpoint.stream_path?.table_id || 'N/A'}
            </Descriptions.Item>
            <Descriptions.Item label="系列">{endpointSeriesLabel(viewingEndpoint)}</Descriptions.Item>
            <Descriptions.Item label="Stream Path">
              {viewingEndpoint.stream_path?.full_path || 'Unknown'}
            </Descriptions.Item>
            <Descriptions.Item label="分辨率">
              <Tag color={viewingEndpoint.resolution === '超清' ? 'purple' : viewingEndpoint.resolution === '高清' ? 'blue' : 'default'}>
                {viewingEndpoint.resolution || '普清'}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="状态">
              <Tag color={viewingEndpoint.status === 1 ? 'green' : 'red'}>
                {viewingEndpoint.status === 1 ? '已启用' : '已禁用'}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="创建时间">
              {new Date(viewingEndpoint.created_at).toLocaleString()}
            </Descriptions.Item>
            <Descriptions.Item label="更新时间">
              {new Date(viewingEndpoint.updated_at).toLocaleString()}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>

      {/* 播放弹窗 */}
      <Modal
        title={`播放视频流区域 - ${playingEndpoint?.stream?.name || 'Unknown'}`}
        open={playModalVisible}
        onCancel={handleClosePlayModal}
        footer={[
          <Button key="close" onClick={handleClosePlayModal}>
            关闭
          </Button>,
        ]}
        width={800}
        afterOpenChange={(open) => {
          if (!open) {
            // Modal 关闭时清理播放器
            if (flvPlayerRef.current) {
              try {
                flvPlayerRef.current.pause();
                flvPlayerRef.current.unload();
                flvPlayerRef.current.detachMediaElement();
                flvPlayerRef.current.destroy();
              } catch (e) {
                console.error('Error destroying player on close:', e);
              }
              flvPlayerRef.current = null;
            }
          }
        }}
      >
        {playingEndpoint && (
          <div>
            <Descriptions bordered column={1} size="small" style={{ marginBottom: 16 }}>
              <Descriptions.Item label="URL">
                <Space>
                  <a href={playingEndpoint.full_url} target="_blank" rel="noopener noreferrer">
                    {playingEndpoint.full_url}
                  </a>
                  <Button
                    type="text"
                    icon={<CopyOutlined />}
                    size="small"
                    onClick={() => handleCopyUrl(playingEndpoint.full_url)}
                    title="复制URL"
                  />
                </Space>
              </Descriptions.Item>
              <Descriptions.Item label="厂商">
                {playingEndpoint.provider?.name || 'Unknown'}
              </Descriptions.Item>
              <Descriptions.Item label="线路">
                {playingEndpoint.line?.name || 'Unknown'}
              </Descriptions.Item>
              <Descriptions.Item label="域名">
                {playingEndpoint.domain?.name || 'Unknown'}
              </Descriptions.Item>
              <Descriptions.Item label="视频流区域">
                {playingEndpoint.stream?.name || 'Unknown'}
              </Descriptions.Item>
              <Descriptions.Item label="桌台号">
                {playingEndpoint.stream_path?.table_id || 'N/A'}
              </Descriptions.Item>
              <Descriptions.Item label="系列">{endpointSeriesLabel(playingEndpoint)}</Descriptions.Item>
            </Descriptions>
            <div style={{ marginTop: 16, textAlign: 'center' }}>
              {flvjs.isSupported() ? (
                <video
                  ref={videoRef}
                  controls
                  muted
                  style={{ width: '100%', maxHeight: '500px', backgroundColor: '#000', minHeight: '300px' }}
                  playsInline
                />
              ) : (
                <div style={{ padding: '40px', textAlign: 'center', color: '#999' }}>
                  <p>您的浏览器不支持 FLV 播放</p>
                  <p>请使用 Chrome、Firefox 或 Edge 浏览器</p>
                  <p style={{ marginTop: 16 }}>
                    <a href={playingEndpoint.full_url} target="_blank" rel="noopener noreferrer">
                      点击这里在新窗口打开
                    </a>
                  </p>
                </div>
              )}
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}

