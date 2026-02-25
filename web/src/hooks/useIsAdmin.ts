import { useState, useEffect } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { api } from '../lib/api';

/**
 * Returns whether the current user has admin role (from API).
 * Use this for Admin Panel visibility in sidebar, TopBar, Horizon Modeler.
 */
export function useIsAdmin(): boolean {
  const { currentUser } = useAuth();
  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    const load = async () => {
      try {
        const uid = currentUser?.uid;
        if (!uid) {
          setIsAdmin(false);
          return;
        }
        const user = await api.getUser(uid);
        const role = (user as { role?: string })?.role ?? '';
        setIsAdmin(typeof role === 'string' && role.toLowerCase() === 'admin');
      } catch {
        setIsAdmin(false);
      }
    };
    load();
  }, [currentUser?.uid]);

  return isAdmin;
}
