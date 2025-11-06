import { useEffect, useRef } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { useDiscoveryStore } from '../stores/discoveryStore';

/**
 * Hook для управления WebSocket соединением
 */
export function useWebSocketConnection() {
  const { currentUser } = useAuth();
  const { connectWs } = useDiscoveryStore();
  const wsInitialized = useRef(false);

  // Separate effect for WebSocket to prevent duplicate connections
  useEffect(() => {
    if (wsInitialized.current) return;
    
    const tenantId = currentUser?.email || '';
    if (tenantId) {
      wsInitialized.current = true;
      connectWs(tenantId);
    }
  }, [currentUser?.email, connectWs]);
}

