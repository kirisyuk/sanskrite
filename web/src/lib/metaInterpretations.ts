/**
 * Human-readable interpretations for meta-analysis metrics
 */

export interface MetaInsight {
  title: string;
  description: string;
  sentiment: 'positive' | 'neutral' | 'negative';
}

/**
 * Generate key insights from aggregate metrics
 */
export function generateKeyInsights(agg: any): MetaInsight[] {
  const insights: MetaInsight[] = [];
  
  // Effectiveness insight
  const effectiveness = agg.effectiveness || 0;
  if (effectiveness >= 0.7) {
    insights.push({
      title: 'Effective dialog',
      description: 'You maintain clear focus and structure in your conversations',
      sentiment: 'positive'
    });
  } else if (effectiveness >= 0.5) {
    insights.push({
      title: 'Moderate effectiveness',
      description: 'Your dialogs are functional but could benefit from clearer structure',
      sentiment: 'neutral'
    });
  } else {
    insights.push({
      title: 'Room for improvement',
      description: 'Consider being more specific and structured in your requests',
      sentiment: 'negative'
    });
  }

  // Structure insight
  const structure = agg.structureScore || 0;
  if (structure >= 0.7) {
    insights.push({
      title: 'Well-structured requests',
      description: 'Your messages are organized and easy to follow',
      sentiment: 'positive'
    });
  } else if (structure < 0.5) {
    insights.push({
      title: 'Needs more structure',
      description: 'Try breaking complex requests into clear steps or bullet points',
      sentiment: 'negative'
    });
  }

  // Focus retention insight
  const focus = agg.focusRetention || 0;
  if (focus >= 0.7) {
    insights.push({
      title: 'Strong focus',
      description: 'You stay on topic and maintain conversation continuity',
      sentiment: 'positive'
    });
  } else if (focus < 0.5) {
    insights.push({
      title: 'Scattered focus',
      description: 'Topics shift frequently - try maintaining thread continuity',
      sentiment: 'negative'
    });
  }

  // Thinking depth (from lastResults)
  const thinkingDepth = agg.lastResults?.thinking?.depth?.label;
  if (thinkingDepth === 'Systemic' || thinkingDepth === 'Deep') {
    insights.push({
      title: 'Deep thinking',
      description: 'You explore problems thoroughly and consider interconnections',
      sentiment: 'positive'
    });
  } else if (thinkingDepth === 'Shallow' || thinkingDepth === 'Basic') {
    insights.push({
      title: 'Surface-level exploration',
      description: 'Consider diving deeper into problem analysis',
      sentiment: 'neutral'
    });
  }

  return insights.slice(0, 3); // Max 3 insights
}

/**
 * Interpret thinking depth enum
 */
export function interpretThinkingDepth(label: string): string {
  const map: Record<string, string> = {
    'Shallow': 'Surface-level: focusing on immediate, obvious aspects',
    'Basic': 'Basic: understanding key points without deep analysis',
    'Nuanced': 'Nuanced: recognizing subtleties and multiple perspectives',
    'Deep': 'Deep: thorough analysis with consideration of implications',
    'Systemic': 'Systemic: holistic view considering interconnections and patterns'
  };
  return map[label] || label;
}

/**
 * Interpret time horizon enum
 */
export function interpretTimeHorizon(label: string): string {
  const map: Record<string, string> = {
    'Immediate': 'Immediate: focused on current moment and quick results',
    'Tactical': 'Tactical: planning for near-term (days to weeks)',
    'Strategic': 'Strategic: considering medium to long-term (months to years)',
    'Visionary': 'Visionary: thinking about distant future and transformative change'
  };
  return map[label] || label;
}

/**
 * Interpret uncertainty handling enum
 */
export function interpretUncertainty(label: string): string {
  const map: Record<string, string> = {
    'Ignores': 'Ignores uncertainty: proceeding with assumptions',
    'Acknowledges': 'Acknowledges uncertainty: aware of unknowns',
    'Explores': 'Explores uncertainty: actively investigating unknowns',
    'Embraces': 'Embraces uncertainty: comfortable with ambiguity and multiple scenarios'
  };
  return map[label] || label;
}

/**
 * Interpret decision process enum
 */
export function interpretDecisionProcess(label: string): string {
  const map: Record<string, string> = {
    'Intuitive': 'Intuitive: relying on gut feeling and experience',
    'Analytical': 'Analytical: systematic evaluation of options',
    'Hybrid': 'Hybrid: balancing intuition with analysis'
  };
  return map[label] || label;
}

/**
 * Interpret risk attitude enum
 */
export function interpretRiskAttitude(label: string): string {
  const map: Record<string, string> = {
    'RiskAverse': 'Risk-averse: preferring safe, proven approaches',
    'RiskNeutral': 'Risk-neutral: balanced approach to risk',
    'RiskSeeking': 'Risk-seeking: willing to try novel or uncertain approaches'
  };
  return map[label] || label;
}

/**
 * Interpret criteria style enum
 */
export function interpretCriteriaStyle(label: string): string {
  const map: Record<string, string> = {
    'Rigid': 'Rigid: sticking to predefined criteria',
    'Adaptive': 'Adaptive: adjusting criteria based on context',
    'Flexible': 'Flexible: open to changing evaluation approach'
  };
  return map[label] || label;
}

/**
 * Interpret communication clarity enum
 */
export function interpretClarity(label: string): string {
  const map: Record<string, string> = {
    'Vague': 'Vague: unclear or ambiguous communication',
    'Unclear': 'Unclear: some confusion in expression',
    'Clear': 'Clear: well-articulated and understandable',
    'Precise': 'Precise: exact and unambiguous communication'
  };
  return map[label] || label;
}

/**
 * Interpret abstraction level enum
 */
export function interpretAbstraction(label: string): string {
  const map: Record<string, string> = {
    'Concrete': 'Concrete: specific examples and tangible details',
    'Mixed': 'Mixed: combining concrete and abstract thinking',
    'Abstract': 'Abstract: conceptual and theoretical approach',
    'Theoretical': 'Theoretical: highly abstract, principle-based thinking'
  };
  return map[label] || label;
}

/**
 * Interpret context completeness enum
 */
export function interpretContextCompleteness(label: string): string {
  const map: Record<string, string> = {
    'Missing': 'Missing context: insufficient background information',
    'Partial': 'Partial context: some background provided',
    'Sufficient': 'Sufficient context: adequate information shared',
    'Complete': 'Complete context: comprehensive background provided'
  };
  return map[label] || label;
}

/**
 * Interpret learning dynamics enum
 */
export function interpretLearningDynamics(label: string): string {
  const map: Record<string, string> = {
    'Static': 'Static: repeating similar patterns',
    'Incremental': 'Incremental: gradual refinement',
    'Deepening': 'Deepening: progressively more sophisticated',
    'Transformative': 'Transformative: fundamental shifts in approach'
  };
  return map[label] || label;
}

/**
 * Interpret integration level enum
 */
export function interpretIntegration(label: string): string {
  const map: Record<string, string> = {
    'Isolated': 'Isolated: treating topics separately',
    'Connected': 'Connected: linking related concepts',
    'Integrated': 'Integrated: synthesizing across domains',
    'Systemic': 'Systemic: holistic, interconnected understanding'
  };
  return map[label] || label;
}

/**
 * Interpret meta-awareness enum
 */
export function interpretMetaAwareness(label: string): string {
  const map: Record<string, string> = {
    'None': 'No meta-awareness: not reflecting on own thinking',
    'Partial': 'Partial meta-awareness: occasional self-reflection',
    'Aware': 'Meta-aware: conscious of own thinking patterns',
    'Reflective': 'Highly reflective: actively examining and adjusting approach'
  };
  return map[label] || label;
}

/**
 * Get color for metric value (0-1 scale)
 */
export function getMetricColor(value: number): string {
  if (value >= 0.7) return 'hsl(120, 50%, 50%)'; // green
  if (value >= 0.5) return 'hsl(45, 70%, 55%)'; // yellow
  return 'hsl(0, 50%, 50%)'; // red
}

/**
 * Format percentage for display
 */
export function formatPercent(value: number): string {
  return `${Math.round(value * 100)}%`;
}

