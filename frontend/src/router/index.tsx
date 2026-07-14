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
import AdminSecurityDashboardPage from '../pages/admin/security/AdminSecurityDashboardPage';
import AdminSecurityReportsPage from '../pages/admin/security/AdminSecurityReportsPage';
import AdminSecurityScannerConfigPage from '../pages/admin/security/AdminSecurityScannerConfigPage';
import RiskRulesPage from '../pages/admin/RiskRulesPage';
import ModelManagementPage from '../pages/admin/ModelManagementPage';
import SystemSettingsPage from '../pages/admin/SystemSettingsPage';
import RuntimePodsPage from '../pages/admin/RuntimePodsPage';
import UserSettingsPage from '../pages/settings/UserSettingsPage';
import OpenClawConfigCenterPage from '../pages/openclaw/OpenClawConfigCenterPage';
import SecplaneInputDetectionPage from '../pages/admin/secplane/InputDetectionPage';
import SecplaneSecureClawPage from '../pages/admin/secplane/SecureClawPage';
import SecurityProtectionPage from '../pages/admin/secplane/SecurityProtectionPage';
import SecurityEventsPage from '../pages/admin/secplane/SecurityEventsPage';
import RuntimeSecurityCategoryPage from '../pages/admin/secplane/runtime/RuntimeSecurityCategoryPage';
import InputSurfacePage from '../pages/admin/secplane/runtime/InputSurfacePage';
import StateSurfacePage from '../pages/admin/secplane/runtime/StateSurfacePage';
import DecisionSurfacePage from '../pages/admin/secplane/runtime/DecisionSurfacePage';
import OutputSurfacePage from '../pages/admin/secplane/runtime/OutputSurfacePage';
import AssetProtectionPage from '../pages/admin/secplane/runtime/AssetProtectionPage';
import CategoryPage from '../pages/admin/protection/CategoryPage';
import AuditPage from '../pages/admin/protection/scenarios/AuditPage';
import ApprovalPage from '../pages/admin/protection/scenarios/ApprovalPage';
import OutboundPage from '../pages/admin/protection/scenarios/OutboundPage';
import ContainerPage from '../pages/admin/protection/scenarios/ContainerPage';
import PolicyPage from '../pages/admin/protection/scenarios/PolicyPage';
import BreakerPage from '../pages/admin/protection/scenarios/BreakerPage';
import HostHardeningPage from '../pages/admin/protection/scenarios/HostHardeningPage';
import CollaborationGovernancePage from '../pages/admin/protection/scenarios/CollaborationGovernancePage';
import CollaborationQuotaPage from '../pages/admin/protection/scenarios/CollaborationQuotaPage';

// Instance Pages
import InstanceListPage from '../pages/instances/InstanceListPage';
import CreateInstancePage from '../pages/instances/CreateInstancePage';
import InstanceDetailPage from '../pages/instances/InstanceDetailPage';
import InstancePortalPage from '../pages/instances/InstancePortalPage';
import TeamListPage from '../pages/teams/TeamListPage';
import CreateTeamPage from '../pages/teams/CreateTeamPage';
import TeamDetailPage from '../pages/teams/TeamDetailPage';

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
        path="/teams"
        element={
          <ProtectedRoute>
            <TeamListPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/teams/new"
        element={
          <ProtectedRoute>
            <CreateTeamPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/teams/:id"
        element={
          <ProtectedRoute>
            <TeamDetailPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/openclaw-configs"
        element={
          <ProtectedRoute>
            <OpenClawConfigCenterPage />
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
        path="/admin/runtime-pods"
        element={
          <AdminRoute>
            <RuntimePodsPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/security"
        element={
          <AdminRoute>
            <AdminSecurityDashboardPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/security/reports"
        element={
          <AdminRoute>
            <AdminSecurityReportsPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/security/scanner"
        element={
          <AdminRoute>
            <AdminSecurityScannerConfigPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/assets"
        element={<Navigate to="/admin/security" replace />}
      />
      <Route
        path="/admin/skills"
        element={<Navigate to="/admin/security" replace />}
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

      {/* Secplane (Security Protection Platform) Routes */}
      <Route
        path="/admin/secplane/input-detection"
        element={
          <AdminRoute>
            <SecplaneInputDetectionPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/secplane/secureclaw"
        element={
          <AdminRoute>
            <SecplaneSecureClawPage />
          </AdminRoute>
        }
      />
      {/* New runtime-security pages (prototype-aligned). Default secplane
          landing is now the protection overview hub. Legacy input-detection
          and secureclaw routes remain reachable from the sidebar nav. */}
      <Route
        path="/admin/secplane"
        element={
          <AdminRoute>
            <SecurityProtectionPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/secplane/events"
        element={
          <AdminRoute>
            <SecurityEventsPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/secplane/runtime"
        element={
          <AdminRoute>
            <RuntimeSecurityCategoryPage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/secplane/runtime/input"
        element={
          <AdminRoute>
            <InputSurfacePage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/secplane/runtime/state"
        element={
          <AdminRoute>
            <StateSurfacePage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/secplane/runtime/decision"
        element={
          <AdminRoute>
            <DecisionSurfacePage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/secplane/runtime/output"
        element={
          <AdminRoute>
            <OutputSurfacePage />
          </AdminRoute>
        }
      />
      <Route
        path="/admin/secplane/runtime/asset"
        element={
          <AdminRoute>
            <AssetProtectionPage />
          </AdminRoute>
        }
      />
      {/* === KSecForAIDemo 原型对齐：6 个新类目入口（cat-2~7）+ 8 个新 scenario 占位 === */}
      <Route path="/admin/secplane/cat-trust" element={<AdminRoute><CategoryPage catId="cat-4" /></AdminRoute>} />
      <Route path="/admin/secplane/cat-identity" element={<AdminRoute><CategoryPage catId="cat-2" /></AdminRoute>} />
      <Route path="/admin/secplane/cat-isolate" element={<AdminRoute><CategoryPage catId="cat-6" /></AdminRoute>} />
      <Route path="/admin/secplane/cat-govern" element={<AdminRoute><CategoryPage catId="cat-5" /></AdminRoute>} />
      <Route path="/admin/secplane/cat-policy" element={<AdminRoute><CategoryPage catId="cat-7" /></AdminRoute>} />
      <Route path="/admin/secplane/cat-comm" element={<AdminRoute><CategoryPage catId="cat-3" /></AdminRoute>} />
      <Route path="/admin/secplane/runtime/approval" element={<AdminRoute><ApprovalPage /></AdminRoute>} />
      <Route path="/admin/secplane/trust/outbound" element={<AdminRoute><OutboundPage /></AdminRoute>} />
      <Route path="/admin/secplane/govern/breaker" element={<AdminRoute><BreakerPage /></AdminRoute>} />
      <Route path="/admin/secplane/govern/audit" element={<AdminRoute><AuditPage /></AdminRoute>} />
      <Route path="/admin/secplane/isolate/container" element={<AdminRoute><ContainerPage /></AdminRoute>} />
      <Route path="/admin/secplane/isolate/host" element={<AdminRoute><HostHardeningPage /></AdminRoute>} />
      <Route path="/admin/secplane/policy/governance" element={<AdminRoute><PolicyPage /></AdminRoute>} />
      <Route path="/admin/secplane/comm/governance" element={<AdminRoute><CollaborationGovernancePage /></AdminRoute>} />
      <Route path="/admin/secplane/comm/quota" element={<AdminRoute><CollaborationQuotaPage /></AdminRoute>} />
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
