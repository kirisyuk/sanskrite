import React, { createContext, useContext, useEffect, useState } from 'react';
import {
  User,
  signInWithEmailAndPassword,
  createUserWithEmailAndPassword,
  sendPasswordResetEmail,
  signOut as firebaseSignOut,
  onAuthStateChanged,
  deleteUser,
} from 'firebase/auth';
import { auth } from '../lib/firebase';
import { api } from '../lib/api';

interface AuthContextType {
  user: User | null;
  currentUser: User | null;
  loading: boolean;
  signIn: (email: string, password: string) => Promise<void>;
  signUp: (email: string, password: string, accountType: 'personal' | 'corporate', displayName?: string) => Promise<void>;
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

  useEffect(() => {
    const unsubscribe = onAuthStateChanged(auth, async (firebaseUser) => {
      setUser(firebaseUser);
      setLoading(false);
    });

    return unsubscribe;
  }, []);

  const signIn = async (email: string, password: string) => {
    await signInWithEmailAndPassword(auth, email, password);
  };

  const signUp = async (email: string, password: string, accountType: 'personal' | 'corporate', displayName?: string) => {
    let firebaseUser: User | null = null;
    try {
      // Step 1: Create Firebase user
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

  const resetPassword = async (email: string) => {
    await sendPasswordResetEmail(auth, email);
  };

  const signOut = async () => {
    await firebaseSignOut(auth);
  };

  const value = {
    user,
    currentUser: user,
    loading,
    signIn,
    signUp,
    resetPassword,
    signOut,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};

