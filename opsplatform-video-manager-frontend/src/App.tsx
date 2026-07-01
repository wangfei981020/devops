// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import type { ReactElement } from 'react';
import DashboardLayout from './components/DashboardLayout';
import ProtectedRoute from './components/ProtectedRoute';
import LoginPage from './pages/LoginPage';
import DashboardPage from './pages/DashboardPage';
import ProvidersPage from './pages/ProvidersPage';
import LinesPage from './pages/LinesPage';
import DomainsPage from './pages/DomainsPage';
import StreamsPage from './pages/StreamsPage';
import StreamPathsPage from './pages/StreamPathsPage';
import VideoStreamEndpointsPage from './pages/VideoStreamEndpointsPage';
import TokenManagementPage from './pages/TokenManagementPage';
import SwaggerPage from './pages/SwaggerPage';
import SystemSettingsPage from './pages/SystemSettingsPage';
import UsersPage from './pages/UsersPage';
import { auth } from './lib/auth';

interface AdminOnlyRouteProps {
  children: ReactElement;
}

function AdminOnlyRoute({ children }: AdminOnlyRouteProps) {
  const user = auth.getUser();
  if (!user?.is_admin) {
    return <Navigate to="/dashboard" replace />;
  }
  return children;
}

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/*"
          element={
            <ProtectedRoute>
              <DashboardLayout>
                <Routes>
                  <Route path="/" element={<Navigate to="/dashboard" replace />} />
                  <Route path="/dashboard" element={<DashboardPage />} />
                  <Route
                    path="/providers"
                    element={
                      <AdminOnlyRoute>
                        <ProvidersPage />
                      </AdminOnlyRoute>
                    }
                  />
                  <Route
                    path="/lines"
                    element={
                      <AdminOnlyRoute>
                        <LinesPage />
                      </AdminOnlyRoute>
                    }
                  />
                  <Route
                    path="/domains"
                    element={
                      <AdminOnlyRoute>
                        <DomainsPage />
                      </AdminOnlyRoute>
                    }
                  />
                  <Route
                    path="/stream-regions"
                    element={
                      <AdminOnlyRoute>
                        <StreamsPage />
                      </AdminOnlyRoute>
                    }
                  />
                  <Route
                    path="/stream-paths"
                    element={
                      <AdminOnlyRoute>
                        <StreamPathsPage />
                      </AdminOnlyRoute>
                    }
                  />
                  <Route path="/endpoints" element={<VideoStreamEndpointsPage />} />
                  <Route
                    path="/token-management"
                    element={
                      <AdminOnlyRoute>
                        <TokenManagementPage />
                      </AdminOnlyRoute>
                    }
                  />
                  <Route
                    path="/users"
                    element={
                      <AdminOnlyRoute>
                        <UsersPage />
                      </AdminOnlyRoute>
                    }
                  />
                  <Route
                    path="/system-settings"
                    element={
                      <AdminOnlyRoute>
                        <SystemSettingsPage />
                      </AdminOnlyRoute>
                    }
                  />
                  <Route
                    path="/swagger"
                    element={
                      <AdminOnlyRoute>
                        <SwaggerPage />
                      </AdminOnlyRoute>
                    }
                  />
                </Routes>
              </DashboardLayout>
            </ProtectedRoute>
          }
        />
      </Routes>
    </BrowserRouter>
  );
}

export default App;
