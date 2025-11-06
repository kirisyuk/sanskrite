import React from 'react';
import { api } from '../lib/api';
import { useAuth } from '../contexts/AuthContext';
import { ThemeContext } from './ThemeProvider';
import { useUIStore } from '../stores/uiStore';
import { useTranslation } from 'react-i18next';

type TabKey = 'personal' | 'corporate';

// Popular countries with ISO 3166-1 alpha-2 codes
const COUNTRIES = [
  { code: 'US', name: 'United States' },
  { code: 'GB', name: 'United Kingdom' },
  { code: 'CA', name: 'Canada' },
  { code: 'AU', name: 'Australia' },
  { code: 'DE', name: 'Germany' },
  { code: 'FR', name: 'France' },
  { code: 'ES', name: 'Spain' },
  { code: 'IT', name: 'Italy' },
  { code: 'NL', name: 'Netherlands' },
  { code: 'CH', name: 'Switzerland' },
  { code: 'SE', name: 'Sweden' },
  { code: 'NO', name: 'Norway' },
  { code: 'DK', name: 'Denmark' },
  { code: 'PL', name: 'Poland' },
  { code: 'RU', name: 'Russia' },
  { code: 'UA', name: 'Ukraine' },
  { code: 'KZ', name: 'Kazakhstan' },
  { code: 'BY', name: 'Belarus' },
  { code: 'TR', name: 'Turkey' },
  { code: 'JP', name: 'Japan' },
  { code: 'CN', name: 'China' },
  { code: 'IN', name: 'India' },
  { code: 'SG', name: 'Singapore' },
  { code: 'AE', name: 'United Arab Emirates' },
  { code: 'BR', name: 'Brazil' },
  { code: 'MX', name: 'Mexico' },
  { code: 'AR', name: 'Argentina' },
];

export const Settings: React.FC = () => {
  const [active, setActive] = React.useState<TabKey>('personal');
  const [fullName, setFullName] = React.useState('');
  const [language, setLanguage] = React.useState('EN');
  const [countryCode, setCountryCode] = React.useState('');
  const [countryInput, setCountryInput] = React.useState('');
  const [showCountryDropdown, setShowCountryDropdown] = React.useState(false);
  const [parentUserId, setParentUserId] = React.useState('');
  const [gender, setGender] = React.useState('');
  const [age, setAge] = React.useState<number | ''>('');
  const { theme, setTheme } = React.useContext(ThemeContext);
  const [company, setCompany] = React.useState('');
  const [jobTitle, setJobTitle] = React.useState('');
  const [department, setDepartment] = React.useState('');
  const { currentUser } = useAuth();
  const countryInputRef = React.useRef<HTMLInputElement>(null);
  const { i18n, t } = useTranslation();
  const uiLanguage = useUIStore((s) => s.uiLanguage);
  const setUiLanguage = useUIStore((s) => s.setUiLanguage);
  const [isMobile, setIsMobile] = React.useState(false);

  React.useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };
    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  const tabs: { key: TabKey; label: string }[] = [
    { key: 'personal', label: t('settings.tabs.personal') },
    { key: 'corporate', label: t('settings.tabs.corporate') },
  ];

  React.useEffect(() => {
    const storedLang = localStorage.getItem('preferredLanguage');
    if (storedLang && storedLang.length === 2) setLanguage(storedLang);
    
    // Load user data
    if (currentUser?.uid) {
      api.getUser(currentUser.uid).then((user) => {
        if (user.displayName) setFullName(user.displayName);
        if (user.preferredLanguage) setLanguage(user.preferredLanguage);
        if (user.countryOfResidence) {
          setCountryCode(user.countryOfResidence);
          const country = COUNTRIES.find(c => c.code === user.countryOfResidence);
          if (country) setCountryInput(country.name);
        }
        if (user.parentUserId) setParentUserId(user.parentUserId);
        if (user.gender) setGender(user.gender);
        if (typeof user.age === 'number') setAge(user.age);
        if (user.companyRef) setCompany(user.companyRef);
        if (user.jobTitle) setJobTitle(user.jobTitle);
        if (user.department) setDepartment(user.department);
      }).catch(() => {});
    }
  }, [currentUser]);

  const saveLanguage = async (code: string) => {
    try {
      localStorage.setItem('preferredLanguage', code);
      document.documentElement.setAttribute('lang', code.toLowerCase());
      if (currentUser?.uid) {
        await api.updateUser(currentUser.uid, { preferredLanguage: code });
      }
    } catch {}
  };

  const saveCountry = async (code: string) => {
    try {
      if (currentUser?.uid) {
        await api.updateUser(currentUser.uid, { countryOfResidence: code });
      }
    } catch (e) {
      console.error('Failed to save country:', e);
    }
  };

  const handleUiLanguageChange = (code: string) => {
    const raw = (code || 'en').toLowerCase();
    const mapped = raw === 'kz' ? 'kk' : raw;
    setUiLanguage(mapped);
    try { document.documentElement.setAttribute('lang', mapped); } catch {}
    try { i18n.changeLanguage(mapped); } catch {}
  };

  const applyTheme = (t: 'dark' | 'light') => {
    setTheme(t);
  };

  const handleCountryInputChange = (value: string) => {
    setCountryInput(value);
    setShowCountryDropdown(true);
  };

  const handleCountrySelect = (country: { code: string; name: string }) => {
    setCountryCode(country.code);
    setCountryInput(country.name);
    setShowCountryDropdown(false);
    saveCountry(country.code);
  };

  const filteredCountries = React.useMemo(() => {
    if (!countryInput.trim()) return COUNTRIES;
    const query = countryInput.toLowerCase();
    return COUNTRIES.filter(c => 
      c.name.toLowerCase().includes(query) || 
      c.code.toLowerCase().includes(query)
    );
  }, [countryInput]);

  // Close dropdown when clicking outside
  React.useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (countryInputRef.current && !countryInputRef.current.contains(e.target as Node)) {
        setShowCountryDropdown(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  return (
    <div className="rounded-xl" style={{ backgroundColor: 'transparent', padding: '0' }}>
      <div className={isMobile ? 'flex flex-col' : 'flex'} style={{ gap: '16px' }}>
        {/* Tabs column/row */}
        <div className={isMobile ? 'w-full' : 'flex-shrink-0'} style={{ width: isMobile ? '100%' : '220px' }}>
          <div className={isMobile ? 'flex flex-row' : 'flex flex-col'} style={{ gap: '6px' }}>
            {tabs.map(t => (
              <button
                key={t.key}
                className={`text-13 ${isMobile ? 'text-center' : 'text-left'} rounded-lg px-3 py-2 transition-colors`}
                style={{
                  backgroundColor: active === t.key ? 'var(--white-10)' : 'transparent',
                  color: 'var(--text-heading)',
                  ...(isMobile ? { flex: '1 1 0' } : {})
                }}
                onMouseEnter={(e) => { if (active !== t.key) (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { if (active !== t.key) (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => setActive(t.key)}
              >
                {t.label}
              </button>
            ))}
          </div>
        </div>

        {/* Divider */}
        {isMobile ? (
          <div style={{ width: '100%', height: '1px', backgroundColor: 'var(--overlay-line-90)' }} />
        ) : (
          <div style={{ width: '1px', backgroundColor: 'var(--overlay-line-90)' }} />
        )}

        {/* Content full width */}
        <div className="flex-1" style={{ paddingLeft: isMobile ? '0' : '16px' }}>
          {active === 'personal' && (
            <div className="space-y-4">
              <div>
                <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.personal.fullName')}</label>
                <input
                  value={fullName}
                  onChange={e => setFullName(e.target.value)}
                  placeholder={t('settings.personal.fullNamePlaceholder')}
                  className="w-full"
                  style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)', padding: '8px 0', transition: 'all 120ms ease' }}
                  onMouseEnter={(e) => {
                    const el = e.currentTarget as HTMLInputElement;
                    el.style.backgroundColor = 'var(--white-06)';
                    el.style.border = 'none';
                    el.style.borderRadius = '6px';
                    el.style.padding = '8px 8px';
                  }}
                  onMouseLeave={(e) => {
                    const el = e.currentTarget as HTMLInputElement;
                    el.style.backgroundColor = 'transparent';
                    el.style.border = 'none';
                    el.style.borderRadius = '0';
                    el.style.padding = '8px 0';
                  }}
                />
              </div>
              <div>
                <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.personal.language')}</label>
                <select
                  value={language}
                  onChange={(e) => { const code = e.target.value; setLanguage(code); saveLanguage(code); }}
                  className="lang-select"
                  style={{ padding: '8px 12px', width: '33%' }}
                >
                  <option value="EN">EN — English</option>
                  <option value="ES">ES — Español</option>
                  <option value="DE">DE — Deutsch</option>
                  <option value="RU">RU — Русский</option>
                </select>
                <div className="text-12 mt-1" style={{ color: 'var(--text-secondary)' }}>
                  {t('settings.personal.languageDescription')}
                </div>
              </div>
              <div>
                <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.personal.interfaceLanguage')}</label>
                <select
                  value={uiLanguage === 'kk' ? 'kz' : (uiLanguage || 'en')}
                  onChange={(e) => handleUiLanguageChange(e.target.value)}
                  className="lang-select"
                  style={{ padding: '8px 12px', width: '33%' }}
                >
                  <option value="en">EN — English</option>
                  <option value="es">ES — Español</option>
                  <option value="de">DE — Deutsch</option>
                  <option value="ru">RU — Русский</option>
                  <option value="kz">KZ — Қазақ</option>
                </select>
                <div className="text-12 mt-1" style={{ color: 'var(--text-secondary)' }}>
                  {t('settings.personal.interfaceLanguageDescription')}
                </div>
              </div>
              <div>
                <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.personal.countryOfResidence')}</label>
                <div style={{ position: 'relative', width: '66%' }} ref={countryInputRef}>
                  <input
                    value={countryInput}
                    onChange={(e) => handleCountryInputChange(e.target.value)}
                    onFocus={() => setShowCountryDropdown(true)}
                    placeholder={t('settings.personal.selectCountry')}
                    className="w-full"
                    style={{ 
                      backgroundColor: 'transparent', 
                      border: 'none', 
                      outline: 'none', 
                      color: 'var(--text-heading)', 
                      padding: '8px 0',
                      borderBottom: '1px solid var(--overlay-line-90)'
                    }}
                  />
                  {showCountryDropdown && filteredCountries.length > 0 && (
                    <div 
                      className="absolute z-50 w-full mt-1 rounded-lg shadow-lg overflow-hidden"
                      style={{ 
                        backgroundColor: 'var(--bg-elevated)', 
                        border: '1px solid var(--overlay-line-90)',
                        maxHeight: '200px',
                        overflowY: 'auto'
                      }}
                    >
                      {filteredCountries.map((country) => (
                        <div
                          key={country.code}
                          className="px-3 py-2 cursor-pointer transition-colors text-13"
                          style={{ 
                            color: 'var(--text-heading)',
                            backgroundColor: countryCode === country.code ? 'var(--white-10)' : 'transparent'
                          }}
                          onMouseEnter={(e) => { (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--white-10)'; }}
                          onMouseLeave={(e) => { 
                            if (countryCode !== country.code) {
                              (e.currentTarget as HTMLDivElement).style.backgroundColor = 'transparent'; 
                            }
                          }}
                          onClick={() => handleCountrySelect(country)}
                        >
                          {country.name} ({country.code})
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </div>
              <div className="grid" style={{ gap: '16px', gridTemplateColumns: '1fr 1fr 1fr' }}>
                <div>
                  <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.personal.parentUserId')}</label>
                  <input
                    value={parentUserId}
                    onChange={e => setParentUserId(e.target.value)}
                    placeholder={t('settings.personal.parentUserIdPlaceholder')}
                    className="w-full"
                    style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)', padding: '8px 0', borderBottom: '1px solid var(--overlay-line-90)' }}
                    onBlur={async () => { try { if (currentUser?.uid) await api.updateUser(currentUser.uid, { parentUserId: parentUserId || undefined }); } catch {} }}
                  />
                </div>
                <div>
                  <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.personal.gender')}</label>
                  <select
                    value={gender}
                    onChange={async (e) => { const v = e.target.value; setGender(v); try { if (currentUser?.uid) await api.updateUser(currentUser.uid, { gender: v || undefined }); } catch {} }}
                    className="w-full"
                    style={{ padding: '8px 12px' }}
                  >
                    <option value="">{t('settings.personal.genderOptions.empty')}</option>
                    <option value="male">{t('settings.personal.genderOptions.male')}</option>
                    <option value="female">{t('settings.personal.genderOptions.female')}</option>
                    <option value="other">{t('settings.personal.genderOptions.other')}</option>
                  </select>
                </div>
                <div>
                  <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.personal.age')}</label>
                  <input
                    type="number"
                    min={0}
                    max={120}
                    value={age}
                    onChange={(e) => {
                      const val = e.target.value;
                      const num = val === '' ? '' : Number(val);
                      if (num === '' || (!Number.isNaN(num) && num >= 0 && num <= 120)) setAge(num as any);
                    }}
                    onBlur={async () => { try { if (currentUser?.uid) await api.updateUser(currentUser.uid, { age: typeof age === 'number' ? age : undefined }); } catch {} }}
                    placeholder={t('settings.personal.agePlaceholder')}
                    className="w-full"
                    style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)', padding: '8px 0', borderBottom: '1px solid var(--overlay-line-90)' }}
                  />
                </div>
              </div>
              <div>
                <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.personal.theme')}</label>
                <div className="flex" style={{ gap: '8px' }}>
                  <button
                    className="text-13 rounded px-3 py-2 transition-colors"
                    style={{ backgroundColor: theme === 'dark' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
                    onClick={() => applyTheme('dark')}
                  >{t('settings.personal.dark')}</button>
                  <button
                    className="text-13 rounded px-3 py-2 transition-colors"
                    style={{ backgroundColor: theme === 'light' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
                    onClick={() => applyTheme('light')}
                  >{t('settings.personal.light')}</button>
                </div>
              </div>
            </div>
          )}
          {active === 'corporate' && (
            <div className="space-y-4">
              <div>
                <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.corporate.company')}</label>
                <input
                  value={company}
                  onChange={e => setCompany(e.target.value)}
                  placeholder={t('settings.corporate.companyPlaceholder')}
                  className="w-full"
                  style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)', padding: '8px 0' }}
                />
              </div>
              <div>
                <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.corporate.jobTitle')}</label>
                <input
                  value={jobTitle}
                  onChange={e => setJobTitle(e.target.value)}
                  placeholder={t('settings.corporate.jobTitlePlaceholder')}
                  className="w-full"
                  style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)', padding: '8px 0' }}
                />
              </div>
              <div>
                <label className="block text-13" style={{ color: 'var(--text-secondary-strong)', marginBottom: '6px' }}>{t('settings.corporate.department')}</label>
                <input
                  value={department}
                  onChange={e => setDepartment(e.target.value)}
                  placeholder={t('settings.corporate.departmentPlaceholder')}
                  className="w-full"
                  style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)', padding: '8px 0' }}
                />
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default Settings;


