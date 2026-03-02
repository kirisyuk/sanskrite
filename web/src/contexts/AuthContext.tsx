import React, { createContext, useContext, useEffect, useState } from 'react';
import {
  User,
  signInWithEmailAndPassword,
  createUserWithEmailAndPassword,
  sendPasswordResetEmail,
  signOut as firebaseSignOut,
  onAuthStateChanged,
  deleteUser,
  signInWithPopup,
} from 'firebase/auth';
import { auth, googleAuthProvider } from '../lib/firebase';
import { api } from '../lib/api';
import { cleanupDiscoveryStore } from '../stores/discoveryStore';

type SubscriptionTier = 'free' | 'pro' | 'max';

interface Subscription {
  id: string;
  tenantId: string;
  userId: string;
  tier: SubscriptionTier;
  status: 'active' | 'expired' | 'cancelled';
  startedAt: string;
  currentPeriodStart: string;
  currentPeriodEnd: string;
  nextBillingDate?: string;
  voucherId?: string;
  paymentProvider?: string;
  paymentId?: string;
  paypalPlanId?: string;
  paypalAgreementId?: string;
  paypalSubscriptionId?: string;
  metadata?: Record<string, any>;
  createdAt: string;
  updatedAt: string;
}

interface AuthContextType {
  user: User | null;
  currentUser: User | null;
  loading: boolean;
  subscription: Subscription | null;
  subscriptionLoading: boolean;
  refreshSubscription: () => Promise<void>;
  signIn: (email: string, password: string) => Promise<void>;
  signUp: (email: string, password: string, accountType: 'personal' | 'corporate', displayName?: string) => Promise<void>;
  signInWithGoogle: () => Promise<void>;
  resetPassword: (email: string) => Promise<void>;
  signOut: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider');
  }
  return context;
};

export const AuthProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [subscription, setSubscription] = useState<Subscription | null>(null);
  const [subscriptionLoading, setSubscriptionLoading] = useState(false);
  const subscriptionTimeoutRef = React.useRef<number | null>(null);

  // Fetch subscription when user is authenticated
  const fetchSubscription = async (firebaseUser: User | null) => {
    if (!firebaseUser?.uid) {
      setSubscription(null);
      return;
    }
    
    setSubscriptionLoading(true);
    try {
      const sub = await api.getSubscription();
      setSubscription(sub);
    } catch (error) {
      console.error('[AuthContext] Failed to fetch subscription:', error);
      // Don't set subscription to null on error - keep previous value
      // This prevents clearing subscription if there's a temporary network issue
    } finally {
      setSubscriptionLoading(false);
    }
  };

  useEffect(() => {
    if (!auth) {
      setLoading(false);
      return;
    }
    const unsubscribe = onAuthStateChanged(auth, async (firebaseUser) => {
      // Clear any pending subscription timeout
      if (subscriptionTimeoutRef.current !== null) {
        clearTimeout(subscriptionTimeoutRef.current);
        subscriptionTimeoutRef.current = null;
      }
      
      setUser(firebaseUser);
      setLoading(false);
      
      // Fetch subscription after authentication
      if (firebaseUser) {
        // Small delay to ensure token is available
        subscriptionTimeoutRef.current = window.setTimeout(() => {
          fetchSubscription(firebaseUser);
          subscriptionTimeoutRef.current = null;
        }, 100);
      } else {
        setSubscription(null);
      }
    });
    
    return () => {
      unsubscribe();
      // Cleanup timeout on unmount
      if (subscriptionTimeoutRef.current !== null) {
        clearTimeout(subscriptionTimeoutRef.current);
        subscriptionTimeoutRef.current = null;
      }
    };
  }, []);

  // Refresh subscription manually (e.g., after applying voucher)
  const refreshSubscription = async () => {
    await fetchSubscription(user);
  };

  const signIn = async (email: string, password: string) => {
    if (!auth) throw new Error('Auth is not initialized');
    await signInWithEmailAndPassword(auth, email, password);
  };

  const signUp = async (email: string, password: string, accountType: 'personal' | 'corporate', displayName?: string) => {
    let firebaseUser: User | null = null;
    try {
      // 0) Waitlist check (must be approved)
      try {
        const res = await (await import('../lib/api')).api.waitlistCheck(email);
        if (!res.found || (res.status || '').toLowerCase() !== 'approved') {
          throw new Error('This email is not approved for Private Beta. Please join the waitlist.');
        }
      } catch (e: any) {
        throw e;
      }

      // Step 1: Create Firebase user
      if (!auth) throw new Error('Auth is not initialized');
      const userCredential = await createUserWithEmailAndPassword(auth, email, password);
      firebaseUser = userCredential.user;

      // Step 2: Create user in backend
      await api.createUser({
        firebaseUid: firebaseUser.uid,
        email: firebaseUser.email!,
        accountType,
        displayName: displayName || undefined,
      });

      console.log('✅ User successfully created in both Firebase and backend');
    } catch (error: any) {
      // Rollback: delete Firebase user if backend creation failed
      if (firebaseUser) {
        console.error('❌ Backend user creation failed, rolling back Firebase user:', error);
        try {
          await deleteUser(firebaseUser);
          console.log('✅ Firebase user rolled back');
        } catch (deleteError) {
          console.error('❌ Failed to rollback Firebase user:', deleteError);
        }
      }
      throw error;
    }
  };

  const signInWithGoogle = async () => {
    if (!auth) throw new Error('Auth is not initialized');
    
    const userCredential = await signInWithPopup(auth, googleAuthProvider);
    const firebaseUser = userCredential.user;
    const email = firebaseUser.email;
    
    if (!email) {
      throw new Error('Google account does not have an email address');
    }

    // Check if user exists in backend
    try {
      await api.getUser(firebaseUser.uid);
      // User exists, authentication complete
      console.log('✅ User signed in with Google');
    } catch (error: any) {
      // User doesn't exist in backend, need to create
      // Check waitlist first
      try {
        const res = await api.waitlistCheck(email);
        if (!res.found || (res.status || '').toLowerCase() !== 'approved') {
          // Rollback: sign out and delete Firebase user
          await firebaseSignOut(auth);
          if (firebaseUser) {
            try {
              await deleteUser(firebaseUser);
            } catch (deleteError) {
              console.error('Failed to rollback Firebase user:', deleteError);
            }
          }
          throw new Error('This email is not approved for Private Beta. Please join the waitlist.');
        }
      } catch (e: any) {
        // Rollback on waitlist check failure
        await firebaseSignOut(auth);
        if (firebaseUser) {
          try {
            await deleteUser(firebaseUser);
          } catch (deleteError) {
            console.error('Failed to rollback Firebase user:', deleteError);
          }
        }
        throw e;
      }

      // Determine account type based on email domain
      const PUBLIC_DOMAINS = [
        'gmail.com', 'yahoo.com', 'hotmail.com', 'outlook.com', 'aol.com',
        'icloud.com', 'mail.com', 'protonmail.com', 'yandex.ru', 'mail.ru',
        'live.com', 'msn.com', 'me.com', 'mac.com'
      ];
      const domain = email.split('@')[1]?.toLowerCase();
      const accountType: 'personal' | 'corporate' = PUBLIC_DOMAINS.includes(domain || '') ? 'personal' : 'corporate';

      // Create user in backend
      try {
        await api.createUser({
          firebaseUid: firebaseUser.uid,
          email: email,
          accountType,
          displayName: firebaseUser.displayName || undefined,
        });
        console.log('✅ User successfully created in backend after Google sign-in');
      } catch (createError: any) {
        // Rollback: sign out and delete Firebase user
        await firebaseSignOut(auth);
        if (firebaseUser) {
          try {
            await deleteUser(firebaseUser);
          } catch (deleteError) {
            console.error('Failed to rollback Firebase user:', deleteError);
          }
        }
        throw createError;
      }
    }
  };

  const resetPassword = async (email: string) => {
    if (!auth) throw new Error('Auth is not initialized');
    await sendPasswordResetEmail(auth, email);
  };

  const signOut = async () => {
    if (!auth) return;
    try {
      cleanupDiscoveryStore();
    } catch (e) {
      console.warn('[Auth] cleanupDiscoveryStore failed', e);
    }
    await firebaseSignOut(auth);
  };

  const value = {
    user,
    currentUser: user,
    loading,
    subscription,
    subscriptionLoading,
    refreshSubscription,
    signIn,
    signUp,
    signInWithGoogle,
    resetPassword,
    signOut,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};

