import { Component, type ReactNode } from 'react';
import { DEFAULT_LOCALE, translate, type Locale } from '../lib/i18n';

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('Error caught by boundary:', error, errorInfo);
  }

  render() {
    if (this.state.hasError) {
      const locale = (window.localStorage.getItem('clawmanager_locale') as Locale | null) ?? DEFAULT_LOCALE;
      return (
        <div style={{ padding: '20px', fontFamily: 'sans-serif' }}>
          <h1>{translate(locale, 'errorBoundary.title') ?? translate(DEFAULT_LOCALE, 'errorBoundary.title') ?? 'Something went wrong'}</h1>
          <pre style={{ background: '#f5f5f5', padding: '10px', overflow: 'auto' }}>
            {this.state.error?.toString()}
          </pre>
        </div>
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
