import { initializeApp } from 'firebase/app';
import { getAuth, GoogleAuthProvider, type Auth } from 'firebase/auth';

const firebaseConfig = {
  apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
  authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
  projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
  storageBucket: import.meta.env.VITE_FIREBASE_STORAGE_BUCKET,
  messagingSenderId: import.meta.env.VITE_FIREBASE_MESSAGING_SENDER_ID,
  appId: import.meta.env.VITE_FIREBASE_APP_ID,
};

let firebaseInitError: Error | null = null;
let app: ReturnType<typeof initializeApp> | null = null;
let authInstance: Auth | null = null;

try {
  app = initializeApp(firebaseConfig);
  try {
    authInstance = getAuth(app);
  } catch (e) {
    firebaseInitError = e as Error;
    authInstance = null;
  }
} catch (e) {
  firebaseInitError = e as Error;
  app = null;
  authInstance = null;
}

export const auth: Auth | null = authInstance;
export const googleAuthProvider = new GoogleAuthProvider();
export function getFirebaseInitError(): Error | null {
  return firebaseInitError;
}
export default app;
