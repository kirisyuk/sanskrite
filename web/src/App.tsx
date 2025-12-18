import React, { useState, useEffect } from 'react';
import './i18n';
import { BrowserRouter as Router, Routes, Route, useLocation, useNavigate } from 'react-router-dom';
import { ThemeProvider } from './components/ThemeProvider';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import AuthGuard from './components/AuthGuard';
import { AuthLoading } from './components/AuthLoading';
import { TopBar } from './components/TopBar';
import { BottomBar } from './components/BottomBar';
import { ServerUnavailableBanner } from './components/ServerUnavailableBanner';
import DiscoveryPage from './pages/DiscoveryPage';
import HorizonPage from './pages/HorizonPage';
import ShareViewPage from './pages/ShareViewPage';
import VerifiedPage from './pages/VerifiedPage';
import VerifiedViewPage from './pages/VerifiedViewPage';
import Homepage from './pages/homepage';
import TermsOfUsage from './pages/TermsOfUsage';
import PrivacyNotice from './pages/PrivacyNotice';
import LegalModal from './components/LegalModal';
import StartupError from './components/StartupError';
import { getFirebaseInitError } from './lib/firebase';

function AppContent() {
  const location = useLocation();
  const navigate = useNavigate();
  const [showModal, setShowModal] = useState<'terms' | 'privacy' | null>(null);
  const { user, loading } = useAuth();

  // Check if we're on a legal page route
  const isLegalPage = location.pathname === '/terms' || location.pathname === '/privacy';
  
  // Determine which page to show
  const isUnfold = new URLSearchParams(location.search).get('app') === 'unfold';
  
  // Handle modal from query parameter
  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const modal = params.get('modal');
    if (modal === 'terms' || modal === 'privacy') {
      setShowModal(modal);
    } else {
      setShowModal(null);
    }
  }, [location.search]);

  // Handle legal page routes - show modal
  useEffect(() => {
    if (location.pathname === '/terms') {
      setShowModal('terms');
    } else if (location.pathname === '/privacy') {
      setShowModal('privacy');
    }
  }, [location.pathname]);

  const handleCloseModal = () => {
    setShowModal(null);
    // If we're on a legal page route, go back to the previous page
    if (isLegalPage) {
      navigate(-1);
    } else {
      // Otherwise just remove the modal query param
      const params = new URLSearchParams(location.search);
      params.delete('modal');
      const newSearch = params.toString();
      navigate({ search: newSearch ? `?${newSearch}` : '' }, { replace: true });
    }
  };

  // Show loading state while checking authentication
  if (loading) {
    return <AuthLoading />;
  }

  return (
    <div className="layout-main">
      <div className="main-content">
        <TopBar />
        <ServerUnavailableBanner />
        <div className="content-area">
          <Routes>
            <Route path="/shared/:token" element={<ShareViewPage />} />
            <Route path="/verified" element={<VerifiedPage />} />
            <Route path="/verified/:code" element={<VerifiedViewPage />} />
            <Route path="/horizon" element={<AuthGuard><HorizonPage /></AuthGuard>} />
            <Route path="/" element={isUnfold ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (user ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (<Homepage />))} />
            <Route path="/terms" element={isUnfold ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (<Homepage />)} />
            <Route path="/privacy" element={isUnfold ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (<Homepage />)} />
          </Routes>
        </div>
        <BottomBar />
        
        {/* Modal overlay */}
        {showModal && <LegalModal type={showModal} onClose={handleCloseModal} />}
      </div>
    </div>
  );
}

export default function App() {
  const firebaseError = getFirebaseInitError();
  
  return (
    <ThemeProvider>
      {firebaseError ? (
        <StartupError error={firebaseError} />
      ) : (
        <Router>
          <AuthProvider>
            <AppContent />
          </AuthProvider>
        </Router>
      )}
    </ThemeProvider>
  );
}
