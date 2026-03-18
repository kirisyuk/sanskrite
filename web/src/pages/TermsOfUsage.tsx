import { useEffect } from 'react';

export default function TermsOfUsage() {
  // Update page title for SEO
  useEffect(() => {
    document.title = 'Terms of Usage - Ideoma LLC';
    const metaDescription = document.querySelector('meta[name="description"]');
    if (metaDescription) {
      metaDescription.setAttribute('content', 'Terms of Usage for Ideoma LLC AI-powered platform. Read our terms and conditions.');
    } else {
      const meta = document.createElement('meta');
      meta.name = 'description';
      meta.content = 'Terms of Usage for Ideoma LLC AI-powered platform. Read our terms and conditions.';
      document.head.appendChild(meta);
    }
  }, []);

  return (
    <div className="max-w-4xl mx-auto px-6 py-8" style={{ color: 'var(--text-primary)', backgroundColor: 'var(--bg-elevated)', borderRadius: 12 }}>
      <h1 className="text-24 font-semibold mb-6" style={{ color: 'var(--text-heading)' }}>Terms of Usage</h1>
      
      <div className="text-13 leading-relaxed space-y-6" style={{ color: 'var(--text-secondary-strong)' }}>
        <p className="text-11" style={{ color: 'var(--text-tertiary)' }}>
          <strong>Last Updated:</strong> October 23, 2025
        </p>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>1. Agreement to Terms</h2>
          <p>
            These Terms of Usage ("Terms") constitute a legally binding agreement between you ("User", "you", or "your") and Ideoma LLC ("Company", "we", "us", or "our"), 
            a limited liability company organized under the laws of the Republic of Kazakhstan, with its principal place of business at Astana, Dostyk, 16, Talan Towers, 
            Republic of Kazakhstan.
          </p>
          <p className="mt-2">
            By accessing, browsing, or using this application (the "Service"), you acknowledge that you have read, understood, and agree to be bound by these Terms 
            and all applicable laws and regulations. If you do not agree to these Terms, you must immediately cease all use of the Service.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>2. Service Description and Modifications</h2>
          <p>
            The Service is an artificial intelligence-powered platform that provides information analysis, content generation, and related features. The Company reserves 
            the absolute right to modify, suspend, discontinue, or restrict access to any part or all of the Service at any time, with or without notice, for any reason 
            or no reason, without liability to you or any third party.
          </p>
          <p className="mt-2">
            We may update these Terms at any time without prior notice. Your continued use of the Service after any such changes constitutes your acceptance of the 
            new Terms. It is your responsibility to review these Terms periodically.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>3. User Eligibility and Account Responsibilities</h2>
          <p>
            The Service is intended for users who are at least 18 years of age. Users under 18 years of age may only access the Service through a parent-controlled 
            account with explicit parental invitation and consent (see Section 4 below).
          </p>
          <p className="mt-2">
            You represent and warrant that: (a) if you are 18 or older, you have the legal capacity to enter into binding contracts; (b) you are not located 
            in a country subject to a governmental embargo or designated as a "terrorist supporting" country; (c) you are not listed on any government list of prohibited 
            or restricted parties; (d) you will comply with all applicable local, state, national, and international laws and regulations.
          </p>
          <p className="mt-2">
            You are solely responsible for maintaining the confidentiality of your account credentials and for all activities that occur under your account. You agree 
            to immediately notify us of any unauthorized use of your account. The Company is not liable for any loss or damage arising from your failure to protect 
            your account information.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>4. Children's Access and Parental Controls</h2>
          
          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>4.1 Age Requirements and Parental Consent</h3>
          <p>
            Children under the age of 18 ("Minors") may ONLY access the Service through a parent-controlled account established by their parent or legal guardian 
            ("Parent"). Independent registration by Minors is strictly prohibited.
          </p>
          <p className="mt-2">
            To provide access to a Minor, Parents must: (a) create an account and complete the parental consent process; (b) provide the Minor's age; (c) explicitly 
            invite the Minor to use the Service under their supervision; (d) acknowledge and accept responsibility for all activities conducted under the Minor's 
            sub-account.
          </p>
          <p className="mt-2">
            By providing access to a Minor, Parents represent and warrant that: (a) they are the Minor's parent or legal guardian with authority to provide consent; 
            (b) they have read and agree to these Terms on behalf of the Minor; (c) they will supervise the Minor's use of the Service; (d) they accept full 
            responsibility for the Minor's compliance with these Terms.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>4.2 Age-Appropriate Content Adaptation</h3>
          <p>
            The Service implements age-appropriate content adaptation based on the age profile provided by Parents. AI responses are tailored to be suitable for 
            the specified age group. However, the Company makes NO GUARANTEE that all content will be perfectly age-appropriate, as AI models may occasionally 
            produce unexpected outputs.
          </p>
          <p className="mt-2">
            We use system instructions and content filtering to guide AI behavior toward child-safe topics and language. The AI is instructed to avoid discussions 
            of violence, adult topics, self-harm, politics, or other potentially harmful subjects. If inappropriate queries are detected, the system will attempt 
            to redirect the conversation to safer topics.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>4.3 Automated Content Moderation</h3>
          <p>
            The Service employs automated content moderation systems to detect potentially dangerous discussion threads, including but not limited to topics 
            related to self-harm, suicide, violence, abuse, inappropriate content, or other harmful subjects.
          </p>
          <p className="mt-2">
            <strong>IMPORTANT LIMITATION:</strong> While we implement multiple layers of protection, including input filtering (analyzing user queries), output 
            filtering (analyzing AI responses), and behavioral monitoring, these systems are NOT PERFECT. Large Language Models are probabilistic systems that 
            may occasionally produce unexpected or inappropriate content despite our safeguards.
          </p>
          <p className="mt-2">
            <strong>NO GUARANTEE OF PERFECT MODERATION:</strong> The Company CANNOT and DOES NOT guarantee that all inappropriate content will be detected or 
            blocked. Parents acknowledge and accept that: (a) automated moderation has inherent limitations; (b) some inappropriate content may bypass filters; 
            (c) AI models may misinterpret context or produce unintended outputs; (d) the Company is not liable for any content that evades our moderation systems.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>4.4 Parental Responsibility and Supervision</h3>
          <p>
            <strong>Parents bear PRIMARY RESPONSIBILITY for supervising their child's use of the Service.</strong> The Service's safety features are designed 
            to assist Parents, not to replace parental supervision.
          </p>
          <p className="mt-2">
            Parents are responsible for: (a) monitoring their child's interactions with the Service; (b) reviewing conversation histories when available; 
            (c) setting appropriate usage boundaries and time limits; (d) educating their child about online safety; (e) immediately reporting any concerning 
            content or behavior to the Company; (f) deciding whether the Service is appropriate for their child.
          </p>
          <p className="mt-2">
            Parents may access conversation histories, configure safety settings, and revoke their child's access at any time through their parent account.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>4.5 Crisis Situations and Emergency Resources</h3>
          <p>
            If our automated systems detect discussions that may indicate a crisis situation (such as self-harm or suicidal ideation), we may: (a) display 
            emergency contact information and crisis helpline numbers; (b) attempt to redirect the conversation to safer topics; (c) notify the Parent if such 
            notification features are enabled.
          </p>
          <p className="mt-2">
            <strong>THE SERVICE IS NOT A CRISIS INTERVENTION TOOL.</strong> In any emergency situation, Parents and Minors should immediately contact local 
            emergency services, crisis hotlines, or mental health professionals. Do not rely on the Service for emergency assistance.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>4.6 Compliance with Children's Privacy Laws</h3>
          <p>
            The Company complies with applicable children's privacy laws, including the Children's Online Privacy Protection Act (COPPA) in the United States 
            and the General Data Protection Regulation (GDPR) in the European Union. For detailed information about how we collect, use, and protect children's 
            data, please refer to Section 11 of our Privacy Notice.
          </p>

          <h3 className="text-14 font-semibold mb-2 mt-4" style={{ color: 'var(--text-heading)' }}>4.7 Limitation of Liability for Children's Content</h3>
          <p>
            TO THE MAXIMUM EXTENT PERMITTED BY LAW, THE COMPANY DISCLAIMS ALL LIABILITY FOR: (a) any inappropriate content that reaches a Minor despite our 
            safety measures; (b) any psychological, emotional, or other harm resulting from a Minor's use of the Service; (c) any decisions or actions taken 
            by a Minor based on AI Content; (d) failures or limitations of our automated moderation systems; (e) any content created, uploaded, or shared by 
            a Minor through the Service.
          </p>
          <p className="mt-2">
            Parents acknowledge that they assume all risks associated with allowing their child to use an AI-powered service and agree to hold the Company 
            harmless from any claims arising from their child's use of the Service.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>5. Acceptable Use and Prohibited Conduct</h2>
          <p>You agree NOT to use the Service to:</p>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            <li>Violate any applicable laws, regulations, or third-party rights;</li>
            <li>Engage in any activity that is harmful, fraudulent, deceptive, threatening, harassing, defamatory, obscene, or otherwise objectionable;</li>
            <li>Distribute viruses, malware, or any other technologies that may harm the Service, its users, or third parties;</li>
            <li>Attempt to gain unauthorized access to the Service, other accounts, computer systems, or networks;</li>
            <li>Interfere with or disrupt the integrity or performance of the Service;</li>
            <li>Scrape, data mine, or use automated means to access the Service without express written permission;</li>
            <li>Impersonate any person or entity or misrepresent your affiliation with any person or entity;</li>
            <li>Generate, distribute, or facilitate the creation of illegal content, including but not limited to content that infringes intellectual property rights;</li>
            <li>Use the Service for any commercial purpose without our prior written consent;</li>
            <li>Reverse engineer, decompile, disassemble, or otherwise attempt to discover the source code of the Service.</li>
          </ul>
          <p className="mt-2">
            Violation of these acceptable use provisions may result in immediate termination of your access to the Service and may subject you to civil and criminal liability.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>6. AI-Generated Content and No Professional Advice</h2>
          <p>
            <strong>CRITICAL DISCLAIMER:</strong> The Service utilizes artificial intelligence and machine learning technologies to generate content. All outputs, responses, 
            suggestions, analyses, and information provided by the Service (collectively, "AI Content") are generated by algorithms and are NOT verified, endorsed, or 
            guaranteed by the Company.
          </p>
          <p className="mt-2">
            AI Content may contain errors, inaccuracies, omissions, biases, or hallucinations. The Company makes NO REPRESENTATIONS OR WARRANTIES regarding the accuracy, 
            completeness, reliability, currentness, or suitability of any AI Content for any particular purpose.
          </p>
          <p className="mt-2">
            <strong>The Service does NOT provide professional advice.</strong> AI Content should NOT be construed as legal, financial, medical, psychological, tax, 
            investment, business, or any other type of professional advice. You should NOT rely on AI Content as a substitute for professional advice from qualified 
            professionals in the relevant field. Always consult with appropriate licensed professionals before making decisions based on any information obtained from 
            the Service.
          </p>
          <p className="mt-2">
            You acknowledge and agree that: (a) you are solely responsible for evaluating, verifying, and validating all AI Content before taking any action based on it; 
            (b) any reliance on AI Content is at your own risk; (c) the Company has no obligation to verify, correct, update, or monitor AI Content; (d) the Company is 
            not responsible for any decisions you make or actions you take based on AI Content.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>7. Third-Party Services and Content</h2>
          <p>
            The Service may integrate with, rely on, or provide access to third-party artificial intelligence providers, APIs, services, websites, or content 
            (collectively, "Third-Party Services"). These Third-Party Services are not under the Company's control and may be subject to their own terms of service 
            and privacy policies.
          </p>
          <p className="mt-2">
            <strong>Open data attribution:</strong> The Service may use geographic data from{' '}
            <a href="https://www.geonames.org/" className="underline" target="_blank" rel="noopener noreferrer">GeoNames</a>
            . Data © GeoNames and licensed under{' '}
            <a href="https://creativecommons.org/licenses/by/4.0/" className="underline" target="_blank" rel="noopener noreferrer">CC BY</a>.
          </p>
          <p className="mt-2">
            The Company does NOT endorse, warrant, or assume any responsibility for any Third-Party Services. Your use of Third-Party Services is entirely at your 
            own risk. You agree to review and comply with the terms and policies of any Third-Party Services you access through the Service.
          </p>
          <p className="mt-2">
            The Company is not liable for any damages, losses, or issues arising from your use of or inability to use Third-Party Services, including but not limited 
            to changes in Third-Party Services, discontinuation of Third-Party Services, or any claims related to Third-Party Services.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>8. Intellectual Property Rights</h2>
          <p>
            The Service, including all software, algorithms, designs, text, graphics, user interfaces, visual interfaces, trademarks, logos, sounds, music, artwork, 
            and computer code (collectively, "Company Materials"), is owned by or licensed to the Company and is protected by copyright, trademark, patent, trade secret, 
            and other intellectual property laws.
          </p>
          <p className="mt-2">
            Subject to your compliance with these Terms, the Company grants you a limited, non-exclusive, non-transferable, non-sublicensable, revocable license to 
            access and use the Service for your personal, non-commercial use only. All rights not expressly granted are reserved by the Company.
          </p>
          <p className="mt-2">
            You retain ownership of the content you submit to the Service ("User Content"). By submitting User Content, you grant the Company a worldwide, non-exclusive, 
            royalty-free, perpetual, irrevocable, sublicensable, and transferable license to use, reproduce, modify, adapt, publish, translate, create derivative works 
            from, distribute, and display such User Content for the purpose of providing, maintaining, and improving the Service.
          </p>
          <p className="mt-2">
            You represent and warrant that: (a) you own or have the necessary rights to submit User Content; (b) User Content does not infringe any third-party rights; 
            (c) User Content complies with these Terms and all applicable laws.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>9. Disclaimer of Warranties</h2>
          <p>
            <strong>THE SERVICE IS PROVIDED ON AN "AS IS" AND "AS AVAILABLE" BASIS WITHOUT WARRANTIES OF ANY KIND, EITHER EXPRESS OR IMPLIED.</strong>
          </p>
          <p className="mt-2">
            TO THE FULLEST EXTENT PERMITTED BY LAW, THE COMPANY DISCLAIMS ALL WARRANTIES, EXPRESS, IMPLIED, OR STATUTORY, INCLUDING BUT NOT LIMITED TO IMPLIED WARRANTIES 
            OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, TITLE, NON-INFRINGEMENT, ACCURACY, RELIABILITY, AVAILABILITY, SECURITY, AND FREEDOM FROM ERRORS OR 
            VIRUSES.
          </p>
          <p className="mt-2">
            The Company does NOT warrant that: (a) the Service will meet your requirements or expectations; (b) the Service will be uninterrupted, timely, secure, or 
            error-free; (c) the results obtained from the use of the Service will be accurate, reliable, or complete; (d) the quality of any information, products, 
            services, or materials obtained through the Service will meet your expectations; (e) any errors in the Service will be corrected.
          </p>
          <p className="mt-2">
            No oral or written information or advice given by the Company or its representatives shall create a warranty. The Company does not warrant, endorse, guarantee, 
            or assume responsibility for any product or service advertised or offered by a third party through the Service.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>10. Limitation of Liability</h2>
          <p>
            <strong>TO THE MAXIMUM EXTENT PERMITTED BY APPLICABLE LAW, IN NO EVENT SHALL THE COMPANY, ITS AFFILIATES, DIRECTORS, OFFICERS, EMPLOYEES, AGENTS, PARTNERS, 
            SUPPLIERS, OR LICENSORS BE LIABLE FOR ANY INDIRECT, INCIDENTAL, SPECIAL, CONSEQUENTIAL, EXEMPLARY, OR PUNITIVE DAMAGES, INCLUDING BUT NOT LIMITED TO LOSS 
            OF PROFITS, REVENUE, DATA, USE, GOODWILL, OR OTHER INTANGIBLE LOSSES, ARISING OUT OF OR RELATED TO YOUR USE OR INABILITY TO USE THE SERVICE, EVEN IF THE 
            COMPANY HAS BEEN ADVISED OF THE POSSIBILITY OF SUCH DAMAGES.</strong>
          </p>
          <p className="mt-2">
            Without limiting the foregoing, the Company shall not be liable for: (a) any damages arising from reliance on AI Content; (b) any errors, mistakes, or 
            inaccuracies in content; (c) personal injury or property damage resulting from your use of the Service; (d) unauthorized access to or use of our servers 
            or any personal information stored therein; (e) interruption or cessation of transmission to or from the Service; (f) bugs, viruses, or other harmful 
            code transmitted through the Service; (g) any loss or damage arising from Third-Party Services; (h) any actions or decisions you make based on information 
            obtained through the Service.
          </p>
          <p className="mt-2">
            TO THE MAXIMUM EXTENT PERMITTED BY LAW, THE COMPANY'S TOTAL AGGREGATE LIABILITY FOR ALL CLAIMS ARISING OUT OF OR RELATING TO THESE TERMS OR YOUR USE OF 
            THE SERVICE SHALL NOT EXCEED THE GREATER OF (A) THE AMOUNT YOU PAID TO THE COMPANY IN THE TWELVE (12) MONTHS PRECEDING THE EVENT GIVING RISE TO LIABILITY, 
            OR (B) ONE HUNDRED US DOLLARS (USD $100).
          </p>
          <p className="mt-2">
            Some jurisdictions do not allow the exclusion or limitation of certain warranties or liabilities. In such jurisdictions, the Company's liability shall be 
            limited to the greatest extent permitted by law.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>11. Indemnification</h2>
          <p>
            You agree to indemnify, defend, and hold harmless the Company, its affiliates, and their respective directors, officers, employees, agents, partners, 
            suppliers, and licensors from and against any and all claims, liabilities, damages, losses, costs, expenses, or fees (including reasonable attorneys' fees 
            and court costs) arising out of or relating to: (a) your use or misuse of the Service; (b) your violation of these Terms; (c) your violation of any law, 
            regulation, or third-party rights; (d) User Content you submit; (e) any decisions or actions you take based on AI Content; (f) any false, misleading, or 
            inaccurate information you provide.
          </p>
          <p className="mt-2">
            The Company reserves the right to assume the exclusive defense and control of any matter subject to indemnification by you, in which event you will cooperate 
            with the Company in asserting any available defenses. This indemnification obligation will survive the termination of these Terms and your use of the Service.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>12. Data Processing and Privacy</h2>
          <p>
            Your use of the Service is subject to our Privacy Notice. By using the Service, you acknowledge that you have read and understood our Privacy Notice and 
            consent to the collection, use, and disclosure of your information as described therein.
          </p>
          <p className="mt-2">
            The Company processes personal data in accordance with applicable data protection laws. You acknowledge that the Service may involve the processing of 
            personal data across international borders and consent to such cross-border data transfers as necessary for providing the Service.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>13. Term and Termination</h2>
          <p>
            These Terms commence when you first access or use the Service and continue until terminated. The Company may terminate or suspend your access to the Service 
            immediately, without prior notice or liability, for any reason or no reason, including but not limited to breach of these Terms.
          </p>
          <p className="mt-2">
            Upon termination: (a) your right to use the Service will immediately cease; (b) the Company may delete your account and all associated data without liability; 
            (c) provisions that by their nature should survive termination shall survive, including but not limited to ownership provisions, warranty disclaimers, 
            indemnification obligations, and limitations of liability.
          </p>
          <p className="mt-2">
            You may terminate your account at any time by ceasing to use the Service and contacting us to request account deletion. Termination does not relieve you of 
            any obligations incurred prior to termination.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>14. Dispute Resolution and Governing Law</h2>
          <p>
            These Terms shall be governed by and construed in accordance with the laws of the Republic of Kazakhstan, without regard to its conflict of law provisions.
          </p>
          <p className="mt-2">
            Any dispute, controversy, or claim arising out of or relating to these Terms or the Service, including the breach, termination, or validity thereof, shall 
            be resolved through good faith negotiations between the parties. If the parties cannot resolve the dispute through negotiations within thirty (30) days, 
            the dispute shall be submitted to the exclusive jurisdiction of the competent courts of Astana, Republic of Kazakhstan.
          </p>
          <p className="mt-2">
            You agree to waive any right to a jury trial and to waive any right to participate in a class action or representative action against the Company.
          </p>
          <p className="mt-2">
            Notwithstanding the foregoing, the Company may seek injunctive or other equitable relief in any court of competent jurisdiction to protect its intellectual 
            property rights or confidential information.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>15. Export Controls and Sanctions Compliance</h2>
          <p>
            The Service may be subject to export control laws and regulations of the Republic of Kazakhstan, the United States, the European Union, and other jurisdictions. 
            You represent and warrant that: (a) you are not located in, under the control of, or a national or resident of any country subject to comprehensive sanctions; 
            (b) you are not on any government list of prohibited, sanctioned, or restricted parties; (c) you will not use the Service in violation of any export control 
            or sanctions laws.
          </p>
          <p className="mt-2">
            You agree to comply with all applicable export control, sanctions, and import laws and regulations. You will not directly or indirectly export, re-export, 
            or transfer the Service or any technical data derived therefrom to any prohibited destination, entity, or person.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>16. Force Majeure</h2>
          <p>
            The Company shall not be liable for any failure or delay in performing its obligations under these Terms to the extent such failure or delay is caused by 
            circumstances beyond its reasonable control, including but not limited to acts of God, war, terrorism, civil unrest, government actions, epidemics, pandemics, 
            natural disasters, power failures, internet service provider failures, strikes, labor disputes, equipment failures, or failures of third-party service providers.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>17. General Provisions</h2>
          <p>
            <strong>Entire Agreement:</strong> These Terms, together with our Privacy Notice, constitute the entire agreement between you and the Company regarding 
            the Service and supersede all prior agreements, understandings, and communications.
          </p>
          <p className="mt-2">
            <strong>Severability:</strong> If any provision of these Terms is held to be invalid, illegal, or unenforceable, the remaining provisions shall continue 
            in full force and effect. The invalid provision shall be modified to the minimum extent necessary to make it valid and enforceable.
          </p>
          <p className="mt-2">
            <strong>Waiver:</strong> No waiver of any provision of these Terms shall be deemed a further or continuing waiver of such provision or any other provision. 
            The Company's failure to enforce any right or provision of these Terms shall not constitute a waiver of such right or provision.
          </p>
          <p className="mt-2">
            <strong>Assignment:</strong> You may not assign, transfer, or delegate these Terms or your rights and obligations hereunder without the Company's prior 
            written consent. The Company may freely assign these Terms without restriction. Any attempted assignment in violation of this provision is void.
          </p>
          <p className="mt-2">
            <strong>No Agency:</strong> Nothing in these Terms creates any agency, partnership, joint venture, or employment relationship between you and the Company. 
            You have no authority to bind the Company or make representations on its behalf.
          </p>
          <p className="mt-2">
            <strong>Headings:</strong> Section headings are for convenience only and shall not affect the interpretation of these Terms.
          </p>
          <p className="mt-2">
            <strong>Language:</strong> These Terms may be provided in multiple languages for your convenience. In the event of any conflict or inconsistency between 
            language versions, the English version shall prevail.
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>18. Contact Information</h2>
          <p>
            If you have any questions about these Terms or the Service, please contact us at:
          </p>
          <p className="mt-2">
            <strong>Ideoma LLC</strong><br />
            Astana, Dostyk, 16<br />
            Talan Towers<br />
            Republic of Kazakhstan<br />
            Email: hi@etigram.pro
          </p>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>19. Acknowledgment of Understanding</h2>
          <p>
            BY USING THE SERVICE, YOU ACKNOWLEDGE THAT YOU HAVE READ THESE TERMS, UNDERSTAND THEM, AND AGREE TO BE BOUND BY THEM. YOU FURTHER ACKNOWLEDGE THAT THESE 
            TERMS REPRESENT THE COMPLETE AND EXCLUSIVE STATEMENT OF THE AGREEMENT BETWEEN YOU AND THE COMPANY AND SUPERSEDE ANY PROPOSAL OR PRIOR AGREEMENT, ORAL OR 
            WRITTEN, AND ANY OTHER COMMUNICATIONS RELATING TO THE SUBJECT MATTER OF THESE TERMS.
          </p>
        </section>
      </div>
    </div>
  );
}
