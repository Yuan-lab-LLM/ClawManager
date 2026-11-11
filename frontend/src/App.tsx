import Router from './router';
import ErrorBoundary from './components/ErrorBoundary';
import PageVersionNotice from './components/PageVersionNotice';
import { I18nProvider } from './contexts/I18nContext';
import { focusManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';

// A tiled browser window can regain focus without a visibility change.
// Let Query handle enabled checks, stale data and in-flight request deduplication.
focusManager.setEventListener(onFocus => {
  const update = () => onFocus();
  window.addEventListener('focus', update);
  document.addEventListener('visibilitychange', update);
  return () => {
    window.removeEventListener('focus', update);
    document.removeEventListener('visibilitychange', update);
  };
});

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
