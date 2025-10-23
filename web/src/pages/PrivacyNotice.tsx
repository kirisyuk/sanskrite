import { useEffect } from 'react';

export default function PrivacyNotice() {
  // Update page title for SEO
  useEffect(() => {
    document.title = 'Privacy Notice - Ideoma LLC';
    const metaDescription = document.querySelector('meta[name="description"]');
    if (metaDescription) {
      metaDescription.setAttribute('content', 'Privacy Notice for Ideoma LLC. Learn how we collect, use, and protect your personal data.');
    } else {
      const meta = document.createElement('meta');
      meta.name = 'description';
      meta.content = 'Privacy Notice for Ideoma LLC. Learn how we collect, use, and protect your personal data.';
      document.head.appendChild(meta);
    }
  }, []);

  return (
    <div className="max-w-4xl mx-auto px-6 py-8" style={{ color: 'var(--text-primary)', backgroundColor: 'var(--bg-elevated)', borderRadius: 12 }}>
      <h1 className="text-24 font-semibold mb-6" style={{ color: 'var(--text-heading)' }}>Privacy Notice</h1>
      
      <div className="text-13 leading-relaxed space-y-6" style={{ color: 'var(--text-secondary-strong)' }}>
        <p className="text-11" style={{ color: 'var(--text-tertiary)' }}>
          <strong>Last Updated:</strong> October 23, 2025
        </p>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>1. Introduction</h2>
          <p>
            Ideoma LLC ("Company", "we", "us", or "our"), a limited liability company organized under the laws of the Republic of Kazakhstan, with its principal place 
            of business at Astana, Dostyk, 16, Talan Towers, Republic of Kazakhstan, respects your privacy and is committed to protecting your personal data.
          </p>
          <p className="mt-2">
            This Privacy Notice describes how we collect, use, process, disclose, and protect information obtained through your use of our artificial intelligence 
            platform (the "Service"). By accessing or using the Service, you acknowledge that you have read and understood this Privacy Notice and consent to the 
            practices described herein.
          </p>
          <p className="mt-2">
            If you do not agree with this Privacy Notice, you must immediately cease using the Service.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>2. Data Controller and Legal Basis</h2>
          <p>
            Ideoma LLC is the data controller responsible for your personal data. We process personal data in accordance with applicable laws of the Republic of 
            Kazakhstan and, where applicable, international data protection regulations including the General Data Protection Regulation (GDPR) for European users.
          </p>
          <p className="mt-2">
            The legal bases for processing your personal data include: (a) performance of a contract with you; (b) compliance with legal obligations; (c) your consent, 
            where obtained; (d) our legitimate interests in providing, maintaining, improving, and securing the Service, provided such interests are not overridden 
            by your fundamental rights and freedoms.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>3. Information We Collect</h2>
          
          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>3.1 Information You Provide</h3>
          <p>
            We collect information that you voluntarily provide when using the Service, including:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Account Information:</strong> Email address, name, profile information, and authentication credentials (managed through Firebase Authentication);</li>
            <li><strong>User Content:</strong> Text inputs, queries, prompts, documents, files, and any other content you submit to the Service;</li>
            <li><strong>Communications:</strong> Information contained in your communications with us, including support requests, feedback, and correspondence;</li>
            <li><strong>Payment Information:</strong> If applicable, billing details and payment information (processed by third-party payment processors; we do not store complete payment card details).</li>
          </ul>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>3.2 Automatically Collected Information</h3>
          <p>
            When you use the Service, we automatically collect certain information, including:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Usage Data:</strong> Features used, actions taken, frequency and duration of activities, interactions with AI models, search queries, and Service usage patterns;</li>
            <li><strong>Device Information:</strong> Device type, operating system, browser type and version, unique device identifiers, mobile network information;</li>
            <li><strong>Log Data:</strong> IP address, access times, pages viewed, links clicked, referring/exit pages, error logs, and other diagnostic data;</li>
            <li><strong>Cookies and Tracking Technologies:</strong> Data collected through cookies, web beacons, pixels, local storage, and similar technologies (see Section 5).</li>
          </ul>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>3.3 Information from Third Parties</h3>
          <p>
            We may receive information about you from third-party services, including:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Authentication Providers:</strong> Firebase Authentication and other identity verification services;</li>
            <li><strong>AI Service Providers:</strong> Usage and performance data from third-party AI APIs we integrate with;</li>
            <li><strong>Analytics Services:</strong> Aggregated usage statistics and analytics from service providers;</li>
            <li><strong>Third-Party Platforms:</strong> If you connect third-party accounts or services, we may receive information in accordance with their authorization protocols.</li>
          </ul>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>4. How We Use Your Information</h2>
          <p>
            We use collected information for the following purposes:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Service Provision:</strong> To provide, operate, maintain, and deliver the Service features and functionalities you request;</li>
            <li><strong>AI Processing:</strong> To process your inputs through AI models, generate responses, and provide intelligent features;</li>
            <li><strong>Account Management:</strong> To create and manage your account, authenticate users, and facilitate account-related communications;</li>
            <li><strong>Service Improvement:</strong> To analyze usage patterns, identify trends, conduct research and development, and improve the Service's performance, accuracy, and user experience;</li>
            <li><strong>Personalization:</strong> To customize and optimize your experience, including providing relevant suggestions and recommendations;</li>
            <li><strong>Communications:</strong> To send transactional messages, service announcements, technical notices, updates, security alerts, and administrative messages;</li>
            <li><strong>Marketing:</strong> With your consent where required, to send promotional materials, newsletters, and information about new features (you may opt out at any time);</li>
            <li><strong>Customer Support:</strong> To respond to your requests, inquiries, comments, and provide technical support;</li>
            <li><strong>Security and Fraud Prevention:</strong> To detect, prevent, and address fraud, abuse, security incidents, and other harmful or illegal activities;</li>
            <li><strong>Legal Compliance:</strong> To comply with applicable laws, regulations, legal processes, and enforceable governmental requests;</li>
            <li><strong>Business Operations:</strong> To conduct internal operations, including troubleshooting, data analysis, testing, statistical analysis, and business planning;</li>
            <li><strong>Rights Protection:</strong> To enforce our Terms of Usage and protect the rights, property, and safety of the Company, our users, and third parties.</li>
          </ul>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>5. Cookies and Tracking Technologies</h2>
          <p>
            We use cookies, web beacons, pixels, local storage, and similar tracking technologies to collect information about your browsing activities and Service usage.
          </p>
          <p className="mt-2">
            <strong>Types of cookies we use:</strong>
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Essential Cookies:</strong> Necessary for the Service to function, including authentication and security;</li>
            <li><strong>Functional Cookies:</strong> Enable enhanced functionality and personalization;</li>
            <li><strong>Analytics Cookies:</strong> Help us understand how users interact with the Service;</li>
            <li><strong>Advertising Cookies:</strong> May be used to deliver relevant advertisements (if applicable).</li>
          </ul>
          <p className="mt-2">
            You can control cookies through your browser settings. However, disabling cookies may limit your ability to use certain features of the Service. Most 
            browsers are set to accept cookies by default; you must manually disable them if you do not consent to their use.
          </p>
          <p className="mt-2">
            We may use third-party analytics services (e.g., Google Analytics, Firebase Analytics) that use cookies and similar technologies to collect and analyze 
            information about Service use and report on activities and trends. These services may collect information about your use of other websites, apps, and 
            online resources. You can learn about these providers' privacy practices and opt-out options on their respective websites.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>6. How We Share Your Information</h2>
          <p>
            We do not sell, rent, or trade your personal information to third parties for their marketing purposes. We may share your information in the following 
            circumstances:
          </p>
          
          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>6.1 Service Providers</h3>
          <p>
            We engage third-party companies and individuals to facilitate the Service ("Service Providers"). These providers have access to your personal information 
            only to perform specific tasks on our behalf and are obligated not to disclose or use it for other purposes. Service Providers include:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>Cloud infrastructure and hosting providers (e.g., Google Cloud Platform, Firebase);</li>
            <li>AI and machine learning API providers (e.g., OpenAI, Anthropic, Google Gemini, xAI);</li>
            <li>Authentication services;</li>
            <li>Payment processors;</li>
            <li>Analytics and performance monitoring services;</li>
            <li>Customer support and communication platforms;</li>
            <li>Content delivery networks (CDNs).</li>
          </ul>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>6.2 AI Service Providers</h3>
          <p>
            <strong>IMPORTANT:</strong> To provide AI-powered features, we transmit your inputs and content to third-party AI service providers. These providers process 
            your data according to their own privacy policies and terms of service. While we select reputable providers and implement security measures, we cannot 
            control their data handling practices.
          </p>
          <p className="mt-2">
            You acknowledge and consent that your User Content may be transmitted to and processed by third-party AI providers. We recommend reviewing the privacy 
            policies of major AI providers we use:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>OpenAI (ChatGPT): <a href="https://openai.com/privacy" className="underline" target="_blank" rel="noopener noreferrer">https://openai.com/privacy</a></li>
            <li>Anthropic (Claude): <a href="https://www.anthropic.com/privacy" className="underline" target="_blank" rel="noopener noreferrer">https://www.anthropic.com/privacy</a></li>
            <li>Google (Gemini): <a href="https://policies.google.com/privacy" className="underline" target="_blank" rel="noopener noreferrer">https://policies.google.com/privacy</a></li>
            <li>xAI (Grok): <a href="https://x.ai/legal/privacy-policy" className="underline" target="_blank" rel="noopener noreferrer">https://x.ai/legal/privacy-policy</a></li>
          </ul>
          <p className="mt-2">
            Some AI providers may use inputs to train or improve their models unless you opt out or we implement data processing agreements. We endeavor to configure 
            services to minimize such usage, but cannot guarantee it. You should NOT submit highly sensitive, confidential, or personal information through the Service 
            if you are not comfortable with potential processing by AI providers.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>6.3 Business Transfers</h3>
          <p>
            If the Company is involved in a merger, acquisition, asset sale, bankruptcy, reorganization, or similar transaction, your personal information may be 
            transferred, sold, or assigned as part of that transaction. You acknowledge and consent that any successor entity may continue to use your information 
            as set forth in this Privacy Notice.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>6.4 Legal Requirements and Safety</h3>
          <p>
            We may disclose your information if required by law or if we believe in good faith that such disclosure is necessary to:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>Comply with legal obligations, court orders, subpoenas, or legal processes;</li>
            <li>Respond to lawful requests from government authorities, law enforcement, or regulatory agencies;</li>
            <li>Enforce our Terms of Usage and other agreements;</li>
            <li>Protect the rights, property, and safety of the Company, our users, or the public;</li>
            <li>Detect, prevent, or address fraud, security, or technical issues;</li>
            <li>Investigate potential violations of law or our policies.</li>
          </ul>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>6.5 Aggregated and Anonymized Data</h3>
          <p>
            We may share aggregated, de-identified, or anonymized information that does not directly identify you for research, analytics, marketing, or other business 
            purposes. Such data is not considered personal information under this Privacy Notice.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>6.6 With Your Consent</h3>
          <p>
            We may share your information for purposes not described in this Privacy Notice with your explicit consent.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>7. International Data Transfers</h2>
          <p>
            Your information may be transferred to, stored at, and processed in countries other than your country of residence, including the Republic of Kazakhstan, 
            the United States, and other jurisdictions where we, our affiliates, or our Service Providers maintain facilities or operations.
          </p>
          <p className="mt-2">
            These countries may have data protection laws that differ from those of your jurisdiction and may not provide the same level of protection. By using the 
            Service, you acknowledge and consent to the transfer of your information to countries outside your jurisdiction, including countries that may not have 
            been deemed to provide adequate data protection by your local data protection authority.
          </p>
          <p className="mt-2">
            When we transfer personal data internationally, we implement appropriate safeguards such as standard contractual clauses, data processing agreements, 
            and technical and organizational measures to protect your information. However, we cannot guarantee the same level of protection in all jurisdictions.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>8. Data Retention</h2>
          <p>
            We retain your personal information for as long as necessary to fulfill the purposes described in this Privacy Notice, unless a longer retention period 
            is required or permitted by law.
          </p>
          <p className="mt-2">
            The retention period depends on various factors, including:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>The nature and sensitivity of the information;</li>
            <li>The purposes for which we process the information;</li>
            <li>Whether we can achieve those purposes through other means;</li>
            <li>Applicable legal, regulatory, tax, accounting, or other requirements;</li>
            <li>Our legitimate business interests.</li>
          </ul>
          <p className="mt-2">
            Generally, we retain:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Account Information:</strong> For the duration of your account plus a reasonable period thereafter for legal compliance and business purposes;</li>
            <li><strong>User Content:</strong> For as long as necessary to provide the Service and as specified in your account settings, subject to our legitimate interests in maintaining service functionality;</li>
            <li><strong>Usage and Log Data:</strong> Typically for 12-24 months, unless longer retention is required for security, debugging, or legal purposes;</li>
            <li><strong>Communications:</strong> For the duration necessary to address your inquiry plus a reasonable period for record-keeping.</li>
          </ul>
          <p className="mt-2">
            When we no longer need your personal information, we will securely delete or anonymize it. However, we may retain certain information in backup systems 
            for a limited period and may retain anonymized or aggregated data indefinitely.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>9. Data Security</h2>
          <p>
            We implement reasonable technical, administrative, and physical security measures designed to protect your personal information from unauthorized access, 
            disclosure, alteration, and destruction. These measures include:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>Encryption of data in transit using TLS/SSL protocols;</li>
            <li>Encryption of sensitive data at rest;</li>
            <li>Access controls and authentication mechanisms;</li>
            <li>Regular security assessments and monitoring;</li>
            <li>Secure development practices;</li>
            <li>Employee training on data protection;</li>
            <li>Contractual obligations with Service Providers to protect data.</li>
          </ul>
          <p className="mt-2">
            <strong>HOWEVER, NO METHOD OF TRANSMISSION OVER THE INTERNET OR ELECTRONIC STORAGE IS 100% SECURE.</strong> While we strive to protect your personal 
            information, we cannot guarantee absolute security. You acknowledge and accept the inherent security risks of providing information and transmitting data 
            over the internet, and you agree that we have no liability for any unauthorized access to or use of your information.
          </p>
          <p className="mt-2">
            You are responsible for maintaining the confidentiality of your account credentials and for any activities that occur under your account. You agree to 
            immediately notify us of any unauthorized access or use of your account.
          </p>
          <p className="mt-2">
            In the event of a data breach that is likely to result in a high risk to your rights and freedoms, we will notify you and, where required, relevant 
            authorities in accordance with applicable laws.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>10. Your Rights and Choices</h2>
          <p>
            Depending on your jurisdiction and applicable data protection laws, you may have certain rights regarding your personal information:
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>10.1 Access and Portability</h3>
          <p>
            You may have the right to access, review, and obtain a copy of your personal information. You may also have the right to receive your information in a 
            structured, commonly used, and machine-readable format (data portability).
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>10.2 Correction</h3>
          <p>
            You may have the right to correct, update, or amend inaccurate or incomplete personal information. You can update certain account information directly 
            through your account settings.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>10.3 Deletion</h3>
          <p>
            You may have the right to request deletion of your personal information, subject to certain exceptions (e.g., legal obligations, fraud prevention, 
            security purposes). Note that deletion may result in loss of access to the Service and may not be reversible.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>10.4 Objection and Restriction</h3>
          <p>
            You may have the right to object to or restrict certain processing of your personal information, particularly processing based on legitimate interests 
            or for direct marketing purposes.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>10.5 Withdraw Consent</h3>
          <p>
            Where processing is based on your consent, you have the right to withdraw consent at any time. This does not affect the lawfulness of processing based 
            on consent before withdrawal.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>10.6 Opt-Out of Marketing</h3>
          <p>
            You may opt out of receiving promotional emails by following the unsubscribe instructions in those emails. Even if you opt out, we may still send you 
            transactional or administrative messages.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>10.7 Exercise Your Rights</h3>
          <p>
            To exercise any of these rights, please contact us at info@ideoma.io with your request. We will respond within the timeframe required by applicable law 
            (typically within 30 days).
          </p>
          <p className="mt-2">
            We may need to verify your identity before processing your request. We may decline requests that are unreasonable, excessive, technically infeasible, 
            or would compromise the privacy of others. We may charge a reasonable fee for requests that are manifestly unfounded or excessive.
          </p>
          <p className="mt-2">
            <strong>Important Limitation:</strong> While we can delete data under our control, we cannot control or delete data that has been transmitted to third-party 
            AI service providers. You should contact those providers directly regarding data processed on their systems.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>10.8 Complaints</h3>
          <p>
            If you believe we have not adequately addressed your concerns, you have the right to lodge a complaint with a supervisory authority or data protection 
            authority in your jurisdiction. For residents of the European Economic Area, you can find your data protection authority at 
            <a href="https://edpb.europa.eu/about-edpb/about-edpb/members_en" className="underline ml-1" target="_blank" rel="noopener noreferrer">https://edpb.europa.eu/about-edpb/about-edpb/members_en</a>.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>11. Children's Privacy and COPPA/GDPR Compliance</h2>
          
          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.1 Overview and Parental Consent Model</h3>
          <p>
            The Service allows children under the age of 18 ("Minors" or "Children") to access AI-powered features ONLY through a parent-controlled account model. 
            Independent registration by Minors is strictly prohibited.
          </p>
          <p className="mt-2">
            We comply with the Children's Online Privacy Protection Act (COPPA) in the United States, the General Data Protection Regulation (GDPR) in the European 
            Union, and other applicable children's privacy laws. We implement a "verifiable parental consent" model where parents or legal guardians ("Parents") must 
            actively create accounts, provide consent, and invite their children to use the Service.
          </p>
          <p className="mt-2">
            Under GDPR, the age at which a child can provide their own consent for data processing varies by EU member state (typically 13-16 years). For children 
            below this age, parental consent is required. Under COPPA, parental consent is required for children under 13 years of age in the United States.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.2 Information We Collect from Children</h3>
          <p>
            When Parents grant their child access to the Service, we collect the following information about and from the child:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Age Information:</strong> The child's age (provided by the Parent) to enable age-appropriate content adaptation;</li>
            <li><strong>Account Identifier:</strong> A unique identifier linking the child's sub-account to the Parent's account;</li>
            <li><strong>User-Generated Content:</strong> Text inputs, queries, prompts, and conversations with the AI system;</li>
            <li><strong>Usage Data:</strong> Timestamps, session duration, features used, and interaction patterns;</li>
            <li><strong>Device Information:</strong> Device type, operating system, browser type (automatically collected);</li>
            <li><strong>Content Moderation Data:</strong> Results of automated content filtering and safety checks.</li>
          </ul>
          <p className="mt-2">
            We practice <strong>data minimization</strong> and collect only information strictly necessary to provide the Service and ensure child safety. We do NOT 
            collect children's full names, email addresses, phone numbers, photographs, precise geolocation, or other personal identifiers unless explicitly required 
            for a specific parental consent or account recovery purpose and authorized by the Parent.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.3 How We Use Children's Information</h3>
          <p>
            We use children's information for the following purposes:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Service Provision:</strong> To provide AI-powered conversational features and age-appropriate responses;</li>
            <li><strong>Content Adaptation:</strong> To tailor AI behavior, language complexity, and topic appropriateness based on the child's age;</li>
            <li><strong>Safety and Moderation:</strong> To detect potentially harmful content, dangerous discussion threads, or crisis situations through automated moderation;</li>
            <li><strong>Parental Controls:</strong> To enable Parents to review conversation histories, configure safety settings, and monitor their child's activities;</li>
            <li><strong>Service Improvement:</strong> To analyze usage patterns and improve safety features (using aggregated, anonymized data only);</li>
            <li><strong>Legal Compliance:</strong> To comply with applicable laws and respond to legal processes;</li>
            <li><strong>Crisis Intervention:</strong> To display emergency resources if our systems detect concerning content related to self-harm or danger.</li>
          </ul>
          <p className="mt-2">
            <strong>IMPORTANT: We do NOT use children's data for targeted advertising, behavioral profiling, or marketing purposes.</strong>
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.4 Sharing Children's Information with AI Providers</h3>
          <p>
            <strong>CRITICAL DISCLOSURE:</strong> To provide AI-powered features, we transmit children's queries and conversations to third-party AI service providers 
            (such as OpenAI, Anthropic, Google, xAI). These providers process the data to generate responses.
          </p>
          <p className="mt-2">
            We take the following measures to protect children's data when using AI providers:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Opt-Out from Training:</strong> Where available, we configure API settings to prevent children's data from being used to train AI models. 
            For example, OpenAI's API offers options to opt out of data retention for training purposes. We implement these opt-outs whenever technically feasible;</li>
            <li><strong>Data Processing Agreements:</strong> We seek to establish data processing agreements with AI providers that include specific protections for 
            children's data;</li>
            <li><strong>Pseudonymization:</strong> Where possible, we anonymize or pseudonymize identifying information before transmission to AI providers;</li>
            <li><strong>Limited Retention:</strong> We work with providers that commit to limited data retention periods.</li>
          </ul>
          <p className="mt-2">
            <strong>However, we CANNOT guarantee that third-party AI providers will treat children's data in accordance with COPPA, GDPR, or other specific children's 
            privacy laws, as we do not control their systems or practices.</strong> By providing parental consent, Parents acknowledge and accept this risk.
          </p>
          <p className="mt-2">
            We recommend that Parents review the privacy policies of major AI providers we use:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>OpenAI: <a href="https://openai.com/privacy" className="underline" target="_blank" rel="noopener noreferrer">https://openai.com/privacy</a></li>
            <li>Anthropic: <a href="https://www.anthropic.com/privacy" className="underline" target="_blank" rel="noopener noreferrer">https://www.anthropic.com/privacy</a></li>
            <li>Google: <a href="https://policies.google.com/privacy" className="underline" target="_blank" rel="noopener noreferrer">https://policies.google.com/privacy</a></li>
            <li>xAI: <a href="https://x.ai/legal/privacy-policy" className="underline" target="_blank" rel="noopener noreferrer">https://x.ai/legal/privacy-policy</a></li>
          </ul>
          <p className="mt-2">
            <strong>IMPORTANT FTC GUIDANCE:</strong> The Federal Trade Commission (FTC) has determined that disclosing children's personal information to AI providers 
            for model training purposes requires separate, explicit parental consent. We configure our systems to opt out of training where possible, but Parents should 
            be aware that AI providers' policies may change.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.5 Parental Rights and Controls</h3>
          <p>
            Parents have the following rights regarding their child's information:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Right to Review:</strong> Parents may access and review all personal information we have collected from their child by logging into their 
            parent account or contacting us at info@ideoma.io;</li>
            <li><strong>Right to Delete:</strong> Parents may request deletion of their child's personal information at any time. We will delete data within 30 days, 
            subject to legal retention requirements. Note: We cannot delete data already transmitted to third-party AI providers;</li>
            <li><strong>Right to Refuse Further Collection:</strong> Parents may revoke consent and prevent further collection of their child's information by disabling 
            the child's sub-account or deleting it entirely;</li>
            <li><strong>Right to Modify Consent:</strong> Parents may modify safety settings, age information, or other consent parameters at any time;</li>
            <li><strong>Access to Conversation History:</strong> Parents can view their child's conversation histories through the parent dashboard;</li>
            <li><strong>Notification Preferences:</strong> Parents can configure whether they receive notifications about detected concerning content.</li>
          </ul>
          <p className="mt-2">
            To exercise these rights, Parents may use the parent dashboard or contact us at info@ideoma.io with the subject line "Children's Privacy Request."
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.6 Automated Content Moderation and Safety</h3>
          <p>
            We implement multiple layers of automated content moderation to protect children:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Input Filtering:</strong> Children's queries are analyzed for inappropriate content, dangerous topics, or attempts to bypass safety systems;</li>
            <li><strong>Output Filtering:</strong> AI-generated responses are filtered to block age-inappropriate content, violence, adult themes, self-harm discussion, 
            or other harmful material;</li>
            <li><strong>System Instructions:</strong> AI models receive specific instructions to provide age-appropriate, safe, positive responses suitable for children;</li>
            <li><strong>Behavioral Monitoring:</strong> Conversation patterns are monitored for signs of concerning topics or crisis situations;</li>
            <li><strong>Keyword and Phrase Blocking:</strong> Lists of prohibited words and phrases trigger additional scrutiny or conversation redirection.</li>
          </ul>
          <p className="mt-2">
            <strong>CRITICAL LIMITATION:</strong> While we implement industry-standard safety measures, <strong>we CANNOT and DO NOT guarantee perfect content moderation.</strong> 
            Large Language Models are probabilistic systems that may occasionally produce unexpected, inappropriate, or harmful outputs despite our safeguards. Automated 
            moderation systems have inherent limitations and may fail to detect all inappropriate content or may produce false positives.
          </p>
          <p className="mt-2">
            Parents acknowledge that they are responsible for supervising their child's use of the Service and accept the risks associated with AI-generated content.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.7 Data Retention for Children</h3>
          <p>
            We retain children's information for the following periods:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li><strong>Conversation Data:</strong> Retained for the duration of the child's active account plus 30 days, or until the Parent requests deletion, 
            whichever comes first;</li>
            <li><strong>Safety and Moderation Logs:</strong> Retained for up to 12 months for safety analysis and service improvement;</li>
            <li><strong>Account Information:</strong> Retained until the Parent deletes the child's sub-account;</li>
            <li><strong>Anonymized Data:</strong> We may retain aggregated, anonymized data that cannot identify individual children indefinitely for research and 
            service improvement.</li>
          </ul>
          <p className="mt-2">
            When data is deleted, it is removed from active systems and backups within 90 days. However, we cannot control retention by third-party AI providers.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.8 International Data Transfers for Children</h3>
          <p>
            Children's data may be transferred to and processed in countries outside your jurisdiction, including the United States, where our AI providers operate. 
            These countries may not provide the same level of data protection as your home country.
          </p>
          <p className="mt-2">
            We implement safeguards such as Standard Contractual Clauses and data processing agreements to protect children's data during international transfers. 
            However, Parents acknowledge and consent to these cross-border transfers when granting their child access to the Service.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.9 No Sale of Children's Information</h3>
          <p>
            We do NOT sell, rent, or trade children's personal information to third parties for any purpose. We do NOT use children's information for targeted 
            advertising or marketing. We do NOT create behavioral profiles of children for commercial purposes.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.10 Privacy by Design for Children</h3>
          <p>
            We implement "Privacy by Design" principles for children's accounts:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>Default privacy settings are configured for maximum protection;</li>
            <li>Data collection is minimized to only what is necessary;</li>
            <li>Age-appropriate language and interfaces are used;</li>
            <li>Regular security assessments are conducted on children's features;</li>
            <li>Staff handling children's data undergo specialized training.</li>
          </ul>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.11 Prohibited Independent Registration</h3>
          <p>
            Children may NOT independently create accounts or use the Service without explicit parental invitation and supervision. If we become aware of any 
            child using the Service without proper parental consent, we will immediately terminate the account and delete associated data.
          </p>
          <p className="mt-2">
            If you believe a child has provided us information without parental consent, please contact us immediately at info@ideoma.io.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>11.12 Contact for Children's Privacy Questions</h3>
          <p>
            If you have questions about our children's privacy practices, wish to review or delete your child's information, or wish to revoke consent, please 
            contact us:
          </p>
          <p className="mt-2">
            <strong>Ideoma LLC</strong><br />
            Children's Privacy Inquiries<br />
            Astana, Dostyk, 16, Talan Towers<br />
            Republic of Kazakhstan<br />
            Email: info@ideoma.io<br />
            Subject Line: "Children's Privacy"
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>12. Third-Party Links and Services</h2>
          <p>
            The Service may contain links to third-party websites, applications, or services that are not owned or controlled by us. This Privacy Notice does not 
            apply to third-party services.
          </p>
          <p className="mt-2">
            We are not responsible for the privacy practices, content, or security of third-party services. We encourage you to review the privacy policies of any 
            third-party services you access or use.
          </p>
          <p className="mt-2">
            Your interactions with third-party services, including payment processing, authentication, and AI providers, are governed by their respective privacy 
            policies and terms of service. We disclaim all liability for the practices of third parties.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>13. California Privacy Rights (CCPA)</h2>
          <p>
            If you are a California resident, you may have additional rights under the California Consumer Privacy Act (CCPA) and the California Privacy Rights Act (CPRA):
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>Right to know what personal information is collected, used, shared, or sold;</li>
            <li>Right to delete personal information (subject to exceptions);</li>
            <li>Right to opt-out of the sale or sharing of personal information;</li>
            <li>Right to correct inaccurate personal information;</li>
            <li>Right to limit use of sensitive personal information;</li>
            <li>Right to non-discrimination for exercising your rights.</li>
          </ul>
          <p className="mt-2">
            <strong>We do not sell personal information</strong> as defined by the CCPA. However, we may share information with third parties as described in this 
            Privacy Notice, which may constitute "sharing" under certain definitions.
          </p>
          <p className="mt-2">
            To exercise your California privacy rights, contact us at info@ideoma.io. You may designate an authorized agent to make requests on your behalf, subject 
            to verification requirements.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>14. European Economic Area (EEA) and UK Users</h2>
          <p>
            If you are located in the European Economic Area (EEA), the United Kingdom, or Switzerland, you have certain rights under the General Data Protection 
            Regulation (GDPR) and UK GDPR:
          </p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>Right of access to your personal data;</li>
            <li>Right to rectification of inaccurate data;</li>
            <li>Right to erasure ("right to be forgotten") under certain circumstances;</li>
            <li>Right to restrict processing;</li>
            <li>Right to data portability;</li>
            <li>Right to object to processing based on legitimate interests;</li>
            <li>Right to withdraw consent;</li>
            <li>Right to lodge a complaint with a supervisory authority.</li>
          </ul>
          <p className="mt-2">
            For EEA users, the legal bases for processing include: contract performance, legal obligations, consent, and legitimate interests (as described in Section 2).
          </p>
          <p className="mt-2">
            When we transfer personal data outside the EEA, we implement appropriate safeguards such as Standard Contractual Clauses approved by the European Commission.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>15. Changes to This Privacy Notice</h2>
          <p>
            We reserve the right to modify this Privacy Notice at any time, in our sole discretion. Changes will be effective when we post the revised Privacy Notice 
            on the Service. The "Last Updated" date at the top indicates when this Privacy Notice was last revised.
          </p>
          <p className="mt-2">
            We will provide notice of material changes through the Service, by email, or by other means as required by law. However, it is your responsibility to 
            review this Privacy Notice periodically. Your continued use of the Service after changes are posted constitutes your acceptance of the revised Privacy Notice.
          </p>
          <p className="mt-2">
            If you do not agree to the modified Privacy Notice, you must stop using the Service.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>16. Contact Us</h2>
          <p>
            If you have questions, concerns, or requests regarding this Privacy Notice or our privacy practices, please contact us:
          </p>
          <p className="mt-2">
            <strong>Ideoma LLC</strong><br />
            Astana, Dostyk, 16<br />
            Talan Towers<br />
            Republic of Kazakhstan<br />
            Email: info@ideoma.io
          </p>
          <p className="mt-2">
            We will respond to your inquiry within a reasonable timeframe, typically within 30 days as required by applicable law.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>17. Acknowledgment</h2>
          <p>
            BY USING THE SERVICE, YOU ACKNOWLEDGE THAT YOU HAVE READ AND UNDERSTOOD THIS PRIVACY NOTICE AND AGREE TO THE COLLECTION, USE, DISCLOSURE, AND PROCESSING 
            OF YOUR INFORMATION AS DESCRIBED HEREIN. YOU FURTHER ACKNOWLEDGE THE INHERENT RISKS OF TRANSMITTING DATA OVER THE INTERNET AND THE INVOLVEMENT OF THIRD-PARTY 
            SERVICE PROVIDERS, INCLUDING AI PROVIDERS, IN PROCESSING YOUR INFORMATION.
          </p>
        </section>
      </div>
    </div>
  );
}

