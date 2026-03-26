import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AuthProvider, useAuth } from '../contexts/AuthContext';
import { useI18n } from '../contexts/I18nContext';

// Auth Pages
import LoginPage from '../pages/auth/LoginPage';
import RegisterPage from '../pages/auth/RegisterPage';

// Dashboard Pages
import UserDashboard from '../pages/dashboard/UserDashboard';
import AdminDashboard from '../pages/admin/AdminDashboard';

// Admin Pages
import UserManagementPage from '../pages/admin/UserManagementPage';
import InstanceManagementPage from '../pages/admin/InstanceManagementPage';
import AIGatewayPage from '../pages/admin/AIGatewayPage';
import AIAuditPage from '../pages/admin/AIAuditPage';
import CostsPage from '../pages/admin/CostsPage';
import RiskRulesPage from '../pages/admin/RiskRulesPage';
import ModelManagementPage from '../pages/admin/ModelManagementPage';
import SystemSettingsPage from '../pages/admin/SystemSettingsPage';
import UserSettingsPage from '../pages/settings/UserSettingsPage';

// Instance Pages
import InstanceListPage from '../pages/instances/InstanceListPage';
import CreateInstancePage from '../pages/instances/CreateInstancePage';
import InstanceDetailPage from '../pages/instances/InstanceDetailPage';
import InstancePortalPage from '../pages/instances/InstancePortalPage';

// Protected Route Component
const ProtectedRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, isLoading } = useAuth();
  const { t } = useI18n();

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-lg">{t('common.loading')}</div>
      </div>
    );
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
};

// Admin Route Component
const AdminRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, isLoading, user } = useAuth();
  const { t } = useI18n();

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-lg">{t('common.loading')}</div>
      </div>
    );
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  if (user?.role !== 'admin') {
    return <Navigate to="/dashboard" replace />;
  }

  return <>{children}</>;
};

// Public Route Component (redirects to dashboard if already authenticated)
const PublicRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, isLoading, user } = useAuth();
  const { t } = useI18n();

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-lg">{t('common.loading')}</div>
      </div>
    );
  }

  if (isAuthenticated) {
    // Redirect admin to admin dashboard, users to user dashboard
    if (user?.role === 'admin') {
      return <Navigate to="/admin" replace />;
    }
    return <Navigate to="/dashboard" replace />;
  }

  return <>{children}</>;
};

// Dashboard Redirect Component
const DashboardRedirect: React.FC = () => {
  const { user, isLoading } = useAuth();
  const { t } = useI18n();

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-lg">{t('common.loading')}</div>
      </div>
    );
  }

  if (user?.role === 'admin') {
    return <Navigate to="/admin" replace />;
  }
  return <Navigate to="/dashboard" replace />;
};

function AppRoutes() {
  return (
    <Routes>
      {/* Public Routes */}
      <Route
        path="/login"
        element={
          <PublicRoute>
            <LoginPage />
          </PublicRoute>
        }
      />
      <Route
        path="/register"
        element={
          <PublicRoute>
            <RegisterPage />
          </PublicRoute>
        }
      />

      {/* User Routes */}
      <Route
        path="/dashboard"
        element={
          <ProtectedRoute>
            <UserDashboard />
          </ProtectedRoute>
        }
      />

      {/* Instance Routes */}
      <Route
        path="/instances"
        element={
          <ProtectedRoute>
            <InstanceListPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/instances/new"
        element={
          <ProtectedRoute>
            <CreateInstancePage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/instances/:id"
        element={
          <ProtectedRoute>
            <InstanceDetailPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/portal"
        element={
          <ProtectedRoute>
            <InstancePortalPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/settings"
        element={
          <ProtectedRoute>
            <UserSettingsPage />
          </ProtectedRoute>
        }
      />

      {/* Admin Routes */}
      <Route
        path="/admin"
        element={
          <AdminRoute>
            <AdminDashboard />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/users"
        element={
          <AdminRoute>
            <UserManagementPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/instances"
        element={
          <AdminRoute>
            <InstanceManagementPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/ai-gateway"
        element={
          <AdminRoute>
            <AIGatewayPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/ai-audit"
        element={
          <AdminRoute>
            <AIAuditPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/costs"
        element={
          <AdminRoute>
            <CostsPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/risk-rules"
        element={
          <AdminRoute>
            <RiskRulesPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/models"
        element={
          <AdminRoute>
            <ModelManagementPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/settings"
        element={
          <AdminRoute>
            <SystemSettingsPage />
          </AdminRoute>
        }
      />

      {/* Default Redirect */}
      <Route path="/" element={<DashboardRedirect />} />
    </Routes>
  );
}

function Router() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  );
}

export default Router;
