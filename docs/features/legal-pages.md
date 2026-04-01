# Legal Pages Implementation

## Overview

Implemented comprehensive Terms of Usage and Privacy Notice pages with modal display and SEO optimization.

## Features

### 1. Legal Documents
- **Terms of Usage** (`/web/src/pages/TermsOfUsage.tsx`) - 436 lines, 19 sections
  - Maximum liability protection
  - AI-specific disclaimers and limitations
  - **NEW: Comprehensive children's access policy (Section 4)**
    - Parent-controlled account model
    - Age-appropriate content adaptation
    - Automated content moderation with explicit limitations
    - Parental responsibility and supervision requirements
    - Crisis detection and emergency resources
    - COPPA/GDPR compliance
  - Company details: Ideoma LLC, Astana, Kazakhstan
  
- **Privacy Notice** (`/web/src/pages/PrivacyNotice.tsx`) - 660 lines, 17 sections
  - GDPR, CCPA, UK GDPR compliant
  - **NEW: Extensive children's privacy section (Section 11)**
    - Parental consent model with verifiable consent
    - Data minimization principles
    - Detailed disclosure of AI provider data sharing
    - Opt-out from training AI models
    - Parental rights and controls
    - Content moderation systems and limitations
    - FTC guidance compliance
    - No sale of children's data
    - Privacy by Design principles
  - Special attention to AI provider data transmission
  - International data transfer disclosures

### 2. Modal Display
- **Modal Component** (`/web/src/components/LegalModal.tsx`)
  - 70% viewport width
  - 85% viewport height
  - Responsive overlay
  - Close on Escape key
  - Click outside to close
  - Prevents body scroll when open

### 3. Routing & SEO
- **Routes**:
  - `/terms` - Terms of Usage page
  - `/privacy` - Privacy Notice page
  
- **Features**:
  - Direct URL access for crawlers
  - Dynamic page titles and meta descriptions
  - Modal overlay on click from BottomBar
  - Proper browser history management

### 4. SEO Optimization
- **Sitemap** (`/web/public/sitemap.xml`)
  - Homepage, Terms, Privacy pages
  - Priority and change frequency configured
  
- **Robots.txt** (`/web/public/robots.txt`)
  - Allows all legal pages
  - Sitemap reference included
  
- **Meta Tags**:
  - Dynamic title updates
  - Description meta tags
  - Proper semantic HTML

### 5. Integration
- **BottomBar** - Links open modals with proper hrefs for SEO
- **Firebase Config** - Rewrite rules for SPA routing
- **React Router** - Full client-side routing support

## Usage

### For Users
Click "Terms of Usage" or "Privacy Notice" links in the bottom bar to view documents in a modal.

### Direct Links
- Share: `https://yourdomain.com/terms`
- Share: `https://yourdomain.com/privacy`

### For Google
- Sitemap at: `https://yourdomain.com/sitemap.xml`
- Both pages are crawlable and indexable

## Technical Details

### Stack
- React Router DOM v6
- TypeScript
- Tailwind CSS (via design system variables)

### Browser Support
- ESC key to close modal
- Click outside to close
- History navigation support
- Back button closes modal

### Accessibility
- Semantic HTML
- ARIA labels
- Keyboard navigation
- Focus management

## Company Information

**Ideoma LLC**  
Astana, Dostyk, 16  
Talan Towers  
Republic of Kazakhstan  
Email: igor@nordicvision.org

## Children's Safety Features

### Parent-Controlled Access Model
- Children can ONLY access through parent invitation
- No independent registration allowed
- Parent must provide child's age for content adaptation
- Full parental supervision expected

### Content Protection Layers
1. **Input Filtering** - Analyzes child queries
2. **Output Filtering** - Checks AI responses
3. **System Instructions** - Age-appropriate AI behavior
4. **Behavioral Monitoring** - Detects concerning patterns
5. **Keyword Blocking** - Blocks dangerous topics

### Important Limitations
- **NO GUARANTEE of perfect moderation** - LLMs are probabilistic
- Automated systems have inherent limitations
- Some inappropriate content may bypass filters
- Parents bear primary responsibility for supervision

### Parental Rights
- Review all child's information
- Delete child's data at any time
- Revoke consent and access
- View conversation histories
- Configure safety settings
- Receive notifications about concerning content

### AI Provider Data Sharing
- Children's data transmitted to third-party AI providers
- Opt-out from training where available
- Data processing agreements in place
- Cannot guarantee third-party compliance with children's privacy laws
- Parents must review AI provider policies

## Last Updated
October 23, 2025 (Updated with children's access policy)
