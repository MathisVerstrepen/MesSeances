import { absoluteSiteUrl } from '../../app/utils/siteUrl.ts'

// Crawlers that train models, index for AI answers or fetch pages for a user.
// Named groups make the welcome explicit; a crawler that matches a named
// group ignores `*`, so each group repeats the same private-path rules.
export const AI_CRAWLERS = [
  'GPTBot',
  'OAI-SearchBot',
  'ChatGPT-User',
  'ClaudeBot',
  'Claude-SearchBot',
  'Claude-User',
  'PerplexityBot',
  'Perplexity-User',
  'Google-Extended',
  'Applebot-Extended',
  'Amazonbot',
  'meta-externalagent',
  'MistralAI-User',
  'DuckAssistBot',
  'CCBot',
] as const

export const PRIVATE_PATH_PREFIXES = [
  '/admin',
  '/compte',
  '/api/v1/account',
  '/api/v1/auth',
  '/api/v1/internal',
] as const

export function renderRobotsTxt(siteUrl: string): string {
  const rules = [
    'Allow: /',
    ...PRIVATE_PATH_PREFIXES.map((path) => `Disallow: ${path}`),
  ]
  return [
    `# Guide pour agents IA et LLM : ${absoluteSiteUrl(siteUrl, '/llms.txt')}`,
    '',
    'User-agent: *',
    'Content-Signal: search=yes, ai-input=yes, ai-train=yes',
    ...rules,
    '',
    ...AI_CRAWLERS.map((agent) => `User-agent: ${agent}`),
    ...rules,
    '',
    `Sitemap: ${absoluteSiteUrl(siteUrl, '/sitemap.xml')}`,
    '',
  ].join('\n')
}
