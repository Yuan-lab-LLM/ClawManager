import Router from './router';
import ErrorBoundary from './components/ErrorBoundary';
import PageVersionNotice from './components/PageVersionNotice';
import { I18nProvider } from './contexts/I18nContext';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

const queryClient = new QueryClient();

function App() {
  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <I18nProvider>
          <PageVersionNotice />
          <Router />
        </I18nProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  );
}

export default App;
