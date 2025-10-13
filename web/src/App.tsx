import React from 'react';
import './i18n';
import { ThemeProvider } from './components/ThemeProvider';
import { AuthProvider } from './contexts/AuthContext';
import AuthGuard from './components/AuthGuard';
import { TopBar } from './components/TopBar';
import { BottomBar } from './components/BottomBar';
import DiscoveryPage from './pages/DiscoveryPage';
import StartupError from './components/StartupError';
import { getFirebaseInitError } from './lib/firebase';

export default function App() {
  const firebaseError = getFirebaseInitError();
  return (
    <ThemeProvider>
      {firebaseError ? (
        <StartupError error={firebaseError} />
      ) : (
        <AuthProvider>
          <AuthGuard>
            <div className="layout-main">
              <div className="main-content">
                <TopBar />
                <div className="content-area">
                  <DiscoveryPage />
                </div>
                <BottomBar />
              </div>
            </div>
          </AuthGuard>
        </AuthProvider>
      )}
    </ThemeProvider>
  );
}
