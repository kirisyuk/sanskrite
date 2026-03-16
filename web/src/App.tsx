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
import { AdminModal } from './components/AdminModal';
import { useUIStore } from './stores/uiStore';
import { useIsAdmin } from './hooks/useIsAdmin';
import DiscoveryPage from './pages/DiscoveryPage';
import HorizonPage from './pages/HorizonPage';
import ShareViewPage from './pages/ShareViewPage';
import VerifiedPage from './pages/VerifiedPage';
import VerifiedViewPage from './pages/VerifiedViewPage';
import Homepage from './pages/homepage';
import LegalModal from './components/LegalModal';
import StartupError from './components/StartupError';
import { StrategicRecommendationsInbox } from './components/StrategicRecommendationsInbox';
import { getFirebaseInitError } from './lib/firebase';
import HorizonMarketProfilePrintPage from './pages/HorizonMarketProfilePrintPage';
import HorizonOrgProfilePrintPage from './pages/HorizonOrgProfilePrintPage';
import HorizonGovProfilePrintPage from './pages/HorizonGovProfilePrintPage';
import HorizonDynastyProfilePrintPage from './pages/HorizonDynastyProfilePrintPage';
import HorizonTribeProfilePrintPage from './pages/HorizonTribeProfilePrintPage';
import HorizonProgramProfilePrintPage from './pages/HorizonProgramProfilePrintPage';

function AppContent() {
  const location = useLocation();
  const navigate = useNavigate();
  const [showModal, setShowModal] = useState<'terms' | 'privacy' | 'technical-disclosure' | null>(null);
  const { user, loading } = useAuth();
  const showAdminModal = useUIStore((s) => s.showAdminModal);
  const setShowAdminModal = useUIStore((s) => s.setShowAdminModal);
  const isAdmin = useIsAdmin();

  // Check if we're on a legal page route
  const isLegalPage =
    location.pathname === '/terms' ||
    location.pathname === '/privacy' ||
    location.pathname === '/technical-disclosure';
  
  // Determine which page to show
  const isUnfold = new URLSearchParams(location.search).get('app') === 'unfold';

  // Some pages manage their own internal scrolling (chat history, panels, etc.).
  // On mobile this avoids "double scroll" where the whole app page becomes taller than the viewport.
  const lockContentAreaScroll =
    (location.pathname === '/' && (isUnfold || !!user)) ||
    location.pathname === '/horizon';

  const isPrintRoute = location.pathname.startsWith('/horizon/print/');
  
  // Handle modal from query parameter
  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const modal = params.get('modal');
    if (modal === 'terms' || modal === 'privacy' || modal === 'technical-disclosure') {
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
    } else if (location.pathname === '/technical-disclosure') {
      setShowModal('technical-disclosure');
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
        {!isPrintRoute && <TopBar />}
        {!isPrintRoute && <ServerUnavailableBanner />}
        <div
          className="content-area"
          style={lockContentAreaScroll && !isPrintRoute ? { overflowY: 'hidden' } : undefined}
        >
          <Routes>
            <Route path="/shared/:token" element={<ShareViewPage />} />
            <Route path="/verified" element={<VerifiedPage />} />
            <Route path="/verified/:code" element={<VerifiedViewPage />} />
            <Route path="/horizon/print/market/:fragmentId" element={<AuthGuard><HorizonMarketProfilePrintPage /></AuthGuard>} />
            <Route path="/horizon/print/org/:fragmentId" element={<AuthGuard><HorizonOrgProfilePrintPage /></AuthGuard>} />
            <Route path="/horizon/print/gov/:fragmentId" element={<AuthGuard><HorizonGovProfilePrintPage /></AuthGuard>} />
            <Route path="/horizon/print/dynasty/:fragmentId" element={<AuthGuard><HorizonDynastyProfilePrintPage /></AuthGuard>} />
            <Route path="/horizon/print/tribe/:fragmentId" element={<AuthGuard><HorizonTribeProfilePrintPage /></AuthGuard>} />
            <Route path="/horizon/print/program/:fragmentId" element={<AuthGuard><HorizonProgramProfilePrintPage /></AuthGuard>} />
            <Route path="/horizon" element={<AuthGuard><HorizonPage /></AuthGuard>} />
            <Route path="/" element={isUnfold ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (user ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (<Homepage />))} />
            <Route path="/platform" element={isUnfold ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (user ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (<Homepage />))} />
            <Route path="/terms" element={isUnfold ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (<Homepage />)} />
            <Route path="/privacy" element={isUnfold ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (<Homepage />)} />
            <Route path="/technical-disclosure" element={isUnfold ? (<AuthGuard><DiscoveryPage /></AuthGuard>) : (<Homepage />)} />
          </Routes>
        </div>
        {!isPrintRoute && <BottomBar />}
        
        {/* Modal overlay */}
        {showModal && <LegalModal type={showModal} onClose={handleCloseModal} />}
        {/* Admin modal: open from TopBar, sidebar, or Horizon Modeler */}
        {showAdminModal && isAdmin && (
          <AdminModal isOpen={showAdminModal} onClose={() => setShowAdminModal(false)} />
        )}
        <StrategicRecommendationsInbox />
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
