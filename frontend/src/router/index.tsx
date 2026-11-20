import React, { lazy, Suspense } from 'react';
import NorthboundPortalTheme from '../components/NorthboundPortalTheme';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AuthProvider, useAuth } from '../contexts/AuthContext';
import { useI18n } from '../contexts/I18nContext';

// Auth Pages
import LoginPage from '../pages/auth/LoginPage';
import RegisterPage from '../pages/auth/RegisterPage';

// Dashboard Pages
const UserDashboard = lazy(() => import('../pages/dashboard/UserDashboard'));
const AdminDashboard = lazy(() => import('../pages/admin/AdminDashboard'));

// Admin Pages
const UserManagementPage = lazy(() => import('../pages/admin/UserManagementPage'));
const InstanceManagementPage = lazy(() => import('../pages/admin/InstanceManagementPage'));
const AIGatewayPage = lazy(() => import('../pages/admin/AIGatewayPage'));
const AIAuditPage = lazy(() => import('../pages/admin/AIAuditPage'));
const CostsPage = lazy(() => import('../pages/admin/CostsPage'));
const SessionUsageOverviewPage = lazy(() => import('../pages/admin/SessionUsageOverviewPage'));
const AdminSecurityDashboardPage = lazy(() => import('../pages/admin/security/AdminSecurityDashboardPage'));
const AdminSecurityReportsPage = lazy(() => import('../pages/admin/security/AdminSecurityReportsPage'));
const AdminSecurityScannerConfigPage = lazy(() => import('../pages/admin/security/AdminSecurityScannerConfigPage'));
const RiskRulesPage = lazy(() => import('../pages/admin/RiskRulesPage'));
const ModelManagementPage = lazy(() => import('../pages/admin/ModelManagementPage'));
const SystemSettingsPage = lazy(() => import('../pages/admin/SystemSettingsPage'));
const BackupsPage = lazy(() => import('../pages/admin/backups/TeamDataBackupsPage'));
const NorthboundSettingsPage = lazy(() => import('../pages/admin/NorthboundSettingsPage'));
const RuntimePodsPage = lazy(() => import('../pages/admin/RuntimePodsPage'));
const UserSettingsPage = lazy(() => import('../pages/settings/UserSettingsPage'));
const OpenClawConfigCenterPage = lazy(() => import('../pages/openclaw/OpenClawConfigCenterPage'));
const SecplaneInputDetectionPage = lazy(() => import('../pages/admin/secplane/InputDetectionPage'));
const SecplaneSecureClawPage = lazy(() => import('../pages/admin/secplane/SecureClawPage'));
const SecurityProtectionPage = lazy(() => import('../pages/admin/secplane/SecurityProtectionPage'));
const SecurityEventsPage = lazy(() => import('../pages/admin/secplane/SecurityEventsPage'));
const RuntimeSecurityCategoryPage = lazy(() => import('../pages/admin/secplane/runtime/RuntimeSecurityCategoryPage'));
const InputSurfacePage = lazy(() => import('../pages/admin/secplane/runtime/InputSurfacePage'));
const StateSurfacePage = lazy(() => import('../pages/admin/secplane/runtime/StateSurfacePage'));
const DecisionSurfacePage = lazy(() => import('../pages/admin/secplane/runtime/DecisionSurfacePage'));
const OutputSurfacePage = lazy(() => import('../pages/admin/secplane/runtime/OutputSurfacePage'));
const AssetProtectionPage = lazy(() => import('../pages/admin/secplane/runtime/AssetProtectionPage'));
const CategoryPage = lazy(() => import('../pages/admin/protection/CategoryPage'));
const AuditPage = lazy(() => import('../pages/admin/protection/scenarios/AuditPage'));
const ApprovalPage = lazy(() => import('../pages/admin/protection/scenarios/ApprovalPage'));
const OutboundPage = lazy(() => import('../pages/admin/protection/scenarios/OutboundPage'));
const EgressPrivateExceptionPage = lazy(() => import('../pages/admin/protection/scenarios/EgressPrivateExceptionPage'));
const ContainerPage = lazy(() => import('../pages/admin/protection/scenarios/ContainerPage'));
const PolicyPage = lazy(() => import('../pages/admin/protection/scenarios/PolicyPage'));
const BreakerPage = lazy(() => import('../pages/admin/protection/scenarios/BreakerPage'));
const HostHardeningPage = lazy(() => import('../pages/admin/protection/scenarios/HostHardeningPage'));
const CollaborationGovernancePage = lazy(() => import('../pages/admin/protection/scenarios/CollaborationGovernancePage'));
const CollaborationQuotaPage = lazy(() => import('../pages/admin/protection/scenarios/CollaborationQuotaPage'));

// Instance Pages
const InstanceWorkspaceEntry = lazy(() => import('../pages/instances/InstanceWorkspaceEntry'));
const CreateInstancePage = lazy(() => import('../pages/instances/CreateInstancePage'));
const InstanceDetailPage = lazy(() => import('../pages/instances/InstanceDetailPage'));
const InstancePortalPage = lazy(() => import('../pages/instances/InstancePortalPage'));
const SharedInstancePage = lazy(() => import('../pages/instances/SharedInstancePage'));
const IEISystemListInstancesPage = lazy(() => import('../pages/instances/IEISystemListInstancesPage'));
const IEISystemInstancePage = lazy(() => import('../pages/instances/IEISystemInstancePage'));
const TeamListPage = lazy(() => import('../pages/teams/TeamListPage'));
const CreateTeamPage = lazy(() => import('../pages/teams/CreateTeamPage'));
const CustomTeamTemplatesPage = lazy(() => import('../pages/teams/CustomTeamTemplatesPage'));
const TeamDetailPage = lazy(() => import('../pages/teams/TeamDetailPage'));
const IEISystemTeamPage = lazy(() => import('../pages/teams/IEISystemTeamPage'));
const SkillHubPage = lazy(() => import('../pages/skill-hub/SkillHubPage'));

function RouteLoading() {
  const { t } = useI18n();
  return (
    <div className="min-h-screen flex items-center justify-center" role="status">
      <div className="text-lg">{t('common.loading')}</div>
    </div>
  );
}

// Protected Route Component
const ProtectedRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, isLoading } = useAuth();

  if (isLoading) {
    return <RouteLoading />;
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
};

// Admin Route Component
const AdminRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, isLoading, user } = useAuth();

  if (isLoading) {
    return <RouteLoading />;
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

  if (isLoading) {
    return <RouteLoading />;
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

  if (isLoading) {
    return <RouteLoading />;
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
      <Route path="/share/:code" element={<SharedInstancePage />} />
      <Route element={<NorthboundPortalTheme />}>
        <Route element={<IEISystemListInstancesPage />}>
          <Route path="/ieisystem/list-instances" element={null} />
          <Route path="/ieisystem/instances/:id" element={<IEISystemInstancePage />} />
          <Route path="/ieisystem/teams/:id" element={<IEISystemTeamPage />} />
        </Route>
      </Route>

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
            <InstanceWorkspaceEntry />
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
        path="/teams/custom-templates"
        element={
          <ProtectedRoute>
            <CustomTeamTemplatesPage />
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
        path="/skill-hub"
        element={
          <ProtectedRoute>
            <SkillHubPage />
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
      <Route path="/admin/backups" element={<AdminRoute><BackupsPage /></AdminRoute>} />
      <Route path="/admin/backups/:id" element={<AdminRoute><BackupsPage /></AdminRoute>} />
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
        path="/admin/session-usage"
        element={
          <AdminRoute>
            <SessionUsageOverviewPage />
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
      <Route path="/admin/secplane/trust/egress-private" element={<AdminRoute><EgressPrivateExceptionPage /></AdminRoute>} />
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
		path="/admin/northbound"
		element={
		  <AdminRoute>
			<NorthboundSettingsPage />
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
        <Suspense fallback={<RouteLoading />}>
          <AppRoutes />
        </Suspense>
      </AuthProvider>
    </BrowserRouter>
  );
}

export default Router;
