import type { TFunction } from 'i18next';

type Translator = TFunction | ((key: string, options?: Record<string, unknown>) => string);

function extractAuthCode(error: unknown): string {
  if (error && typeof error === 'object' && 'code' in error && typeof (error as { code?: unknown }).code === 'string') {
    return String((error as { code: string }).code).toLowerCase();
  }

  const message =
    error instanceof Error
      ? error.message
      : typeof error === 'string'
        ? error
        : '';
  const match = message.match(/auth\/[a-z-]+/i);
  return match ? match[0].toLowerCase() : '';
}

function extractMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  if (error && typeof error === 'object' && 'message' in error && typeof (error as { message?: unknown }).message === 'string') {
    return String((error as { message: string }).message);
  }
  return '';
}

export function getAuthErrorMessage(error: unknown, t: Translator): string {
  const code = extractAuthCode(error);
  const rawMessage = extractMessage(error).trim();
  const normalizedMessage = rawMessage.toLowerCase();

  switch (code) {
    case 'auth/invalid-credential':
    case 'auth/invalid-login-credentials':
    case 'auth/user-not-found':
    case 'auth/wrong-password':
      return t('unfold.auth.errors.invalidCredentials');
    case 'auth/invalid-email':
      return t('unfold.auth.errors.invalidEmail');
    case 'auth/email-already-in-use':
      return t('unfold.auth.errors.emailAlreadyInUse');
    case 'auth/weak-password':
      return t('unfold.auth.errors.weakPassword');
    case 'auth/user-disabled':
      return t('unfold.auth.errors.userDisabled');
    case 'auth/too-many-requests':
      return t('unfold.auth.errors.tooManyRequests');
    case 'auth/network-request-failed':
      return t('unfold.auth.errors.network');
    case 'auth/popup-closed-by-user':
      return t('unfold.auth.errors.popupClosed');
    case 'auth/popup-blocked':
      return t('unfold.auth.errors.popupBlocked');
    case 'auth/account-exists-with-different-credential':
      return t('unfold.auth.errors.accountExistsWithDifferentProvider');
    case 'auth/operation-not-allowed':
      return t('unfold.auth.errors.signInMethodNotAllowed');
    case 'auth/missing-password':
      return t('unfold.auth.errors.missingPassword');
    case 'auth/missing-email':
      return t('unfold.auth.errors.missingEmail');
    case 'auth/invalid-action-code':
    case 'auth/expired-action-code':
      return t('unfold.auth.errors.invalidResetLink');
    case 'auth/requires-recent-login':
      return t('unfold.auth.errors.requiresRecentLogin');
    default:
      break;
  }

  if (normalizedMessage.includes('not approved for private beta')) {
    return t('unfold.auth.errors.waitlistNotApproved');
  }
  if (normalizedMessage.includes('does not have an email address')) {
    return t('unfold.auth.errors.googleEmailMissing');
  }
  if (normalizedMessage.includes('auth is not initialized')) {
    return t('unfold.auth.errors.authUnavailable');
  }

  if (rawMessage && !rawMessage.includes('auth/') && !rawMessage.startsWith('Firebase: Error')) {
    return rawMessage;
  }

  return t('unfold.auth.errors.generic');
}
