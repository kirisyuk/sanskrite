import React from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import heroBackground from '../assets/top.png';

export default function Homepage() {
  const navigate = useNavigate();
  const { t } = useTranslation();
  const [showJoin, setShowJoin] = React.useState(false);
  const [showSuccess, setShowSuccess] = React.useState(false);
  const [email, setEmail] = React.useState('');
  const [note, setNote] = React.useState('');
  const [sending, setSending] = React.useState(false);

  React.useEffect(() => {
    document.body.classList.add('homepage-route');
    return () => {
      document.body.classList.remove('homepage-route');
    };
  }, []);

  React.useEffect(() => {
    const handleEsc = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (showSuccess) {
          setShowSuccess(false);
        } else if (showJoin) {
          setShowJoin(false);
        }
      }
    };

    window.addEventListener('keydown', handleEsc);
    return () => window.removeEventListener('keydown', handleEsc);
  }, [showJoin, showSuccess]);

  return (
    <div
      className="homepage-landing"
      style={{ '--homepage-bg': `url(${heroBackground})` } as React.CSSProperties}
    >
      <div className="homepage-landing__backdrop" aria-hidden="true" />
      <div className="homepage-landing__glow" aria-hidden="true" />

      <div className="homepage-landing__inner">
        <aside className="homepage-landing__rail">
          <div className="homepage-landing__badge">{t('homepage.railBadge')}</div>
          <div className="homepage-landing__rail-copy">
            <p className="homepage-landing__label">{t('homepage.railLabel')}</p>
            <p className="homepage-landing__rail-text">{t('homepage.accessNote')}</p>
          </div>
        </aside>

        <section className="homepage-landing__hero">
          <p className="homepage-landing__eyebrow">{t('homepage.tagline')}</p>

          <h1
            className="homepage-landing__title"
            aria-label={t('homepage.heroAriaLabel')}
          >
            <span>{t('homepage.heroLine1')}</span>
            <span>{t('homepage.heroLine2')}</span>
            <span>{t('homepage.heroLine3')}</span>
            <span>{t('homepage.heroLine4')}</span>
          </h1>

          <div className="homepage-landing__copy-panel">
            <div className="homepage-landing__copy-strong">{t('homepage.reclaimMind')}</div>
            <div className="homepage-landing__copy-text">{t('homepage.description')}</div>
            <div className="homepage-landing__copy-text">{t('homepage.description2')}</div>
          </div>

          <div className="homepage-landing__actions">
            <button
              type="button"
              className="homepage-landing__action homepage-landing__action--primary"
              onClick={() => setShowJoin(true)}
            >
              {t('homepage.requestAccess')}
            </button>
            <button
              type="button"
              className="homepage-landing__action"
              onClick={() => navigate('/?app=unfold')}
            >
              {t('homepage.signIn')}
            </button>
          </div>

          <a
            className="homepage-landing__external-link"
            href="https://www.prevalent.com/horizon"
            target="_blank"
            rel="noreferrer"
          >
            {t('homepage.moreInfoLink')}
          </a>
        </section>
      </div>

      {showJoin && (
        <div className="homepage-modal" role="dialog" aria-modal="true">
          <div className="homepage-modal__backdrop" onClick={() => setShowJoin(false)} />
          <div className="homepage-modal__card">
            <div className="homepage-modal__kicker">{t('homepage.accessKicker')}</div>
            <div className="homepage-modal__title">{t('homepage.joinWaitlist')}</div>
            <input
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              className="homepage-modal__input"
              placeholder="you@example.com"
            />
            <div className="homepage-modal__hint">{t('homepage.emailPrivacyNote')}</div>
            <textarea
              value={note}
              onChange={(event) => setNote(event.target.value)}
              rows={3}
              className="homepage-modal__input homepage-modal__textarea"
              placeholder={t('homepage.contextPlaceholder')}
            />
            <div className="homepage-modal__actions">
              <button
                type="button"
                className="homepage-modal__button homepage-modal__button--ghost"
                onClick={() => setShowJoin(false)}
              >
                {t('homepage.cancel')}
              </button>
              <button
                type="button"
                className="homepage-modal__button homepage-modal__button--primary"
                disabled={sending || !email}
                onClick={async () => {
                  try {
                    setSending(true);
                    await (await import('../lib/api')).api.waitlistJoin({
                      email,
                      note: note.trim() || undefined,
                    });
                    setShowJoin(false);
                    setShowSuccess(true);
                    setEmail('');
                    setNote('');
                  } finally {
                    setSending(false);
                  }
                }}
              >
                {sending ? t('homepage.sending') : t('homepage.send')}
              </button>
            </div>
          </div>
        </div>
      )}

      {showSuccess && (
        <div className="homepage-modal" role="dialog" aria-modal="true">
          <div className="homepage-modal__backdrop" onClick={() => setShowSuccess(false)} />
          <div className="homepage-modal__card homepage-modal__card--compact">
            <div className="homepage-modal__kicker">{t('homepage.submittedKicker')}</div>
            <div className="homepage-modal__title homepage-modal__title--multiline">
              {t('homepage.submitted')}
            </div>
            <div className="homepage-modal__actions homepage-modal__actions--single">
              <button
                type="button"
                className="homepage-modal__button homepage-modal__button--primary"
                onClick={() => setShowSuccess(false)}
              >
                {t('homepage.close')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
