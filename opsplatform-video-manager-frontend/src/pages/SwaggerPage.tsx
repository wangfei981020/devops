// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { Typography, Alert, Space, Button, Result } from 'antd';
import { BookOutlined, ReloadOutlined } from '@ant-design/icons';
import { useState, useEffect } from 'react';

const { Title } = Typography;

export default function SwaggerPage() {
  const [refreshKey, setRefreshKey] = useState(0);
  const [hasError, setHasError] = useState(false);

  const handleRefresh = () => {
    setRefreshKey((prev) => prev + 1);
    setHasError(false);
  };

  // Use proxy path for Swagger (configured in vite.config.ts)
  // The proxy forwards /swagger requests to the backend
  const swaggerUrl = '/swagger/index.html';

  useEffect(() => {
    // Check if Swagger is available
    fetch('/swagger/index.html')
      .then((response) => {
        if (!response.ok) {
          setHasError(true);
        }
      })
      .catch(() => {
        setHasError(true);
      });
  }, []);

  return (
    <div style={{
      position: 'fixed',
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      zIndex: 1000,
      backgroundColor: '#fff',
      display: 'flex',
      flexDirection: 'column'
    }}>
      {/* 顶部工具栏 */}
      <div style={{
        padding: '12px 24px',
        borderBottom: '1px solid #f0f0f0',
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        backgroundColor: '#fff',
        zIndex: 1001
      }}>
        <Space>
          <BookOutlined style={{ fontSize: 20 }} />
          <Title level={4} style={{ margin: 0 }}>
            API 文档
          </Title>
        </Space>
        <Button icon={<ReloadOutlined />} onClick={handleRefresh}>
          刷新
        </Button>
      </div>

      {/* 内容区域 */}
      <div style={{ flex: 1, overflow: 'hidden', position: 'relative' }}>
        {hasError ? (
          <div style={{
            display: 'flex',
            justifyContent: 'center',
            alignItems: 'center',
            height: '100%',
            padding: '40px'
          }}>
            <Result
              status="warning"
              title="Swagger 文档未生成"
              subTitle="需要先在后端生成 Swagger 文档才能查看 API 文档。"
              extra={[
                <Alert
                  key="instructions"
                  type="info"
                  message="生成 Swagger 文档的步骤："
                  description={
                    <ol style={{ marginTop: 8, paddingLeft: 20 }}>
                      <li>安装 swag CLI: <code>go install github.com/swaggo/swag/cmd/swag@latest</code></li>
                      <li>进入 backend 目录: <code>cd backend</code></li>
                      <li>生成文档: <code>swag init</code></li>
                      <li>重启后端服务</li>
                      <li>刷新此页面</li>
                    </ol>
                  }
                  style={{ marginTop: 16, textAlign: 'left' }}
                />,
                <Button key="refresh" type="primary" icon={<ReloadOutlined />} onClick={handleRefresh}>
                  刷新页面
                </Button>,
              ]}
            />
          </div>
        ) : (
          <iframe
            key={refreshKey}
            src={swaggerUrl}
            style={{
              width: '100%',
              height: '100%',
              border: 'none',
              display: 'block'
            }}
            title="Swagger API Documentation"
            onLoad={() => setHasError(false)}
            onError={() => {
              setHasError(true);
            }}
          />
        )}
      </div>
    </div>
  );
}

