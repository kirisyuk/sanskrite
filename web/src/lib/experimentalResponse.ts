import { api } from './api';
import { getPreferredLanguage, getLanguageName } from './uiUtils';

export interface ExperimentalResponse {
  reasoningChain: string;
  sources: Array<{ url: string; snippet: string }>;
  fullResponse: string;
  userMessage: string;
}

// Track generation state to prevent duplicates
const generatingForMessages = new Set<string>();

/**
 * Generates experimental response using two-step process:
 * 1. gemini_fast with web search to create a detailed reasoning chain with facts
 * 2. grok_fast to generate full response based on reasoning chain
 */
export async function generateExperimentalResponse(
  userMessage: string,
  messageId: string
): Promise<ExperimentalResponse | null> {
  // Prevent duplicate generation
  if (generatingForMessages.has(messageId)) {
    return null;
  }
  
  generatingForMessages.add(messageId);
  
  try {
    const preferredLang = getPreferredLanguage();
    const langName = getLanguageName(preferredLang);
    
    // Get current date for reasoning
    const currentDate = new Date().toISOString().split('T')[0];
    
    // Step 1: Generate detailed reasoning chain with gemini_fast + web search
    const reasoningPrompt = `Develop a detailed reasoning chain for this user query. Think through the question step by step, exploring different angles and perspectives. Use web search to obtain the most current data and facts. Include specific facts, data points, statistics, and concrete information relevant to the query. Consider different perspectives where applicable.

CURRENT DATE: ${currentDate}

IMPORTANT: Use the most recent and relevant data available, especially in knowledge domains where the rate of change is high (technology, current events, science, economics, etc.). Prioritize data from ${currentDate} or the most recent period available.

Structure your reasoning chain as follows:
- Develop your thinking step by step, with each step building on the previous one
- Include specific facts, numbers, statistics, dates, and concrete data points relevant to the query
- Consider different perspectives and angles where applicable
- Use comparisons and concrete examples when relevant
- Formulate and test hypotheses if applicable to the query
- At the end, explicitly list the web page URLs (website addresses starting with http:// or https://) that you found through web search, with brief snippets explaining their relevance

CRITICAL: Make your reasoning concrete and factual. Include specific data, numbers, dates, and verifiable information. Avoid vague formulations. Think deeply and comprehensively about the query.

CRITICAL ABOUT SOURCES: 
- Web search is enabled for this query. Use it to find current information from the web.
- Sources are web page URLs (website addresses) that you found through web search.
- You MUST explicitly list the actual URLs (starting with http:// or https://) of web pages you found via web search in your response text.
- Only include real, actual URLs of web pages that you actually found through web search. Do NOT invent, make up, or fabricate URLs or sources.
- Format sources as: URL - brief description (e.g., "https://example.com/article - explains the statistics on X")
- If you don't have real sources from web search, don't list any rather than inventing them.
- These URLs will be extracted from your response and used by the next model, so they must be actual URLs of web pages you found.

User query:
${userMessage}`;

    const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

    const reasoningResponse = await api.genaiGenerate({
      model: 'gemini_fast',
      prompt: reasoningPrompt,
      webSearchEnabled: true,
      systemInstruction: langDirective,
      maxOutputTokens: 4096,
    });

    if (!reasoningResponse?.content) {
      console.warn('[ExperimentalResponse] No reasoning chain content received');
      return null;
    }

    // Step 2: Generate full response with grok_fast
    // Extract sources from reasoning response for inclusion in prompt
    const sourcesList: Array<{ url: string; snippet: string }> = [];
    
    // Try to extract sources from attributions if available
    if (reasoningResponse.attributions && Array.isArray(reasoningResponse.attributions)) {
      reasoningResponse.attributions.forEach((attr: any) => {
        if (attr.url) {
          sourcesList.push({
            url: attr.url,
            snippet: attr.title || attr.url,
          });
        }
      });
    }
    
    // Try to extract sources from reasoning text (look for URLs and snippets)
    const reasoningText = reasoningResponse.content;
    if (reasoningText) {
      const urlRegex = /(https?:\/\/[^\s\)]+)/g;
      const matches = reasoningText.match(urlRegex);
      if (matches) {
        matches.forEach((url) => {
          // Find snippet after URL (next line or in parentheses)
          const urlIndex = reasoningText.indexOf(url);
          const afterUrl = reasoningText.slice(urlIndex + url.length, urlIndex + url.length + 200);
          const snippetMatch = afterUrl.match(/[:\-]\s*([^\n]+)/);
          const snippet = snippetMatch ? snippetMatch[1].trim() : url;
          
          if (!sourcesList.some((s) => s.url === url)) {
            sourcesList.push({ url, snippet });
          }
        });
      }
    }
    
    const sourcesText = sourcesList.length > 0
      ? `\n\nSOURCES FROM REASONING CHAIN (YOU MUST USE THESE WHEN REFERENCING INFORMATION):\n${sourcesList.map((s, i) => `${i + 1}. ${s.url} - ${s.snippet}`).join('\n')}\n`
      : '';
    
    const fullResponsePrompt = `Here is the original user query: ${userMessage}

Here is a detailed reasoning chain with facts and data related to this query:
${reasoningResponse.content}${sourcesText}

Generate a complete, well-structured response based on the user query - primarily, and the reasoning chain as a secondary source of information. CRITICAL REQUIREMENTS:

1. RESPONSE FORMAT AND STYLE:
   - Analyze the original user query to independently determine the most appropriate response format and style (e.g., essay, blog post, LinkedIn post, analytical discussion, report, philosophical reflection, etc.)
   - Do NOT rely on any style recommendations from the reasoning chain - determine format solely based on the user's query
   - Use appropriate tone, structure, and presentation style for the determined format
   - Match the format to what the user's query naturally suggests

2. USE REASONING CHAIN: Extract and use concrete numbers, statistics, dates, facts, and insights from the reasoning chain. Cite specific figures, percentages, and data points explicitly. Prioritize the most recent data mentioned in the reasoning chain.

3. STRUCTURE: Organize your response logically, drawing from the reasoning chain but adapting it to your determined format. The structure should flow naturally for the chosen format.

4. COMPARISONS: When the reasoning chain includes comparisons, explicitly perform them in your response. State both values and explain the difference.

5. HYPOTHESES: If the reasoning chain includes hypotheses, address them explicitly - test them, verify if they hold, or explain why they might or might not be valid.

6. MANDATORY SOURCE USAGE: If sources are provided above (from the reasoning chain), you MUST use them when referencing information. When citing facts, statistics, or data from the reasoning chain, explicitly reference the corresponding source URL from the provided sources list. Do not use information without attributing it to the appropriate source when sources are available.

7. NO NEW SOURCES: Do not introduce new sources or links that were not in the reasoning chain. Base your response strictly on the reasoning chain and its sources. If sources are provided, use only those sources.

8. COMPLETE ANSWER: Provide a comprehensive answer that incorporates insights from the reasoning chain, while maintaining natural flow, readability, and the determined format/style.

Generate the response now:`;

    const fullResponse = await api.genaiGenerate({
      model: 'grok_fast',
      prompt: fullResponsePrompt,
      systemInstruction: langDirective,
      maxOutputTokens: 8192,
    });

    if (!fullResponse?.content) {
      console.warn('[ExperimentalResponse] No full response content received');
      return null;
    }

    return {
      reasoningChain: reasoningResponse.content,
      sources: sourcesList,
      fullResponse: fullResponse.content,
      userMessage,
    };
  } catch (error) {
    console.error('[ExperimentalResponse] Generation failed', error);
    return null;
  } finally {
    generatingForMessages.delete(messageId);
  }
}

