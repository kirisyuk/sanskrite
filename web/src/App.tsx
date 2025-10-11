import React from 'react';
import './i18n';
import { ThemeProvider } from './components/ThemeProvider';
import { AuthProvider } from './contexts/AuthContext';
import AuthGuard from './components/AuthGuard';
import { TopBar } from './components/TopBar';
import DiscoveryPage from './pages/DiscoveryPage';

export default function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <AuthGuard>
          <div className="layout-main">
            <div className="main-content">
              <TopBar />
              <div className="content-area">
                <DiscoveryPage />
              </div>
            </div>
          </div>
        </AuthGuard>
      </AuthProvider>
    </ThemeProvider>
  );
}
