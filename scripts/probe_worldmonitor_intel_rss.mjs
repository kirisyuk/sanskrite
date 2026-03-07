#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";

const DEFAULT_FEEDS_PATH = "temp-repos/worldmonitor/src/config/feeds.ts";
const DEFAULT_TIMEOUT_MS = 12000;

const stopWords = new Set([
  "the", "and", "for", "with", "from", "into", "over", "under", "after", "before", "amid",
  "about", "their", "this", "that", "will", "says", "say", "new", "news", "analysis", "report",
  "reports", "update", "live", "what", "why", "how", "who", "can", "could", "should", "is",
  "are", "be", "it", "its", "a", "an", "or", "vs", "to", "of", "in", "on", "at", "by",
]);

const tokenRe = /[a-z][a-z0-9-]{2,}/g;
const feedsBlockRe = /export\s+const\s+INTEL_SOURCES\s*:\s*Feed\[\]\s*=\s*\[(.*?)\];/s;
const feedEntryRe =
  /\{\s*name:\s*'([^']+)'\s*,\s*url:\s*(?:rss|railwayRss)\('([^']+)'\)\s*(?:,\s*type:\s*'([^']+)')?\s*\}/g;

const googleNewsSiteRSS = (site, days = 7) =>
  `https://news.google.com/rss/search?q=${encodeURIComponent(`site:${site} when:${days}d`)}&hl=en-US&gl=US&ceid=US:en`;

const expandedFeeds = [
  { name: "BBC World", url: "https://feeds.bbci.co.uk/news/world/rss.xml", type: "mainstream" },
  { name: "BBC Middle East", url: "https://feeds.bbci.co.uk/news/world/middle_east/rss.xml", type: "mainstream" },
  { name: "Al Jazeera", url: "https://www.aljazeera.com/xml/rss/all.xml", type: "mainstream" },
  { name: "Reuters (site feed)", url: googleNewsSiteRSS("reuters.com", 3), type: "wire" },
  { name: "UN News", url: "https://news.un.org/feed/subscribe/en/news/all/rss.xml", type: "gov" },
  { name: "CISA Advisories", url: "https://www.cisa.gov/cybersecurity-advisories/all.xml", type: "cyber" },
  { name: "US-CERT Alerts", url: "https://www.cisa.gov/uscert/ncas/alerts.xml", type: "cyber" },
  { name: "DoD Releases", url: "https://www.defense.gov/DesktopModules/ArticleCS/RSS.ashx?ContentType=1&Site=945&max=20", type: "gov" },
  { name: "WHO News", url: "https://www.who.int/rss-feeds/news-english.xml", type: "gov" },
  { name: "UK MOD GOV.UK", url: "https://www.gov.uk/government/organisations/ministry-of-defence.atom", type: "gov" },
  { name: "UK FCDO GOV.UK", url: "https://www.gov.uk/government/organisations/foreign-commonwealth-development-office.atom", type: "gov" },
  { name: "State Dept (site feed)", url: googleNewsSiteRSS("state.gov", 7), type: "gov" },
  { name: "The Hacker News", url: "https://feeds.feedburner.com/TheHackersNews", type: "cyber" },
  { name: "BleepingComputer", url: "https://www.bleepingcomputer.com/feed/", type: "cyber" },
  { name: "SecurityWeek", url: "https://feeds.feedburner.com/securityweek", type: "cyber" },
  { name: "gCaptain", url: "https://gcaptain.com/feed/", type: "shipping" },
  { name: "War on the Rocks", url: "https://warontherocks.com/feed/", type: "defense" },
  { name: "RealClearDefense", url: "https://www.realcleardefense.com/index.xml", type: "defense" },
  { name: "USNI News", url: "https://news.usni.org/feed", type: "defense" },
  { name: "USGS Earthquakes M4.5+", url: "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/4.5_day.atom", type: "disaster" },
  { name: "ReliefWeb (site feed)", url: googleNewsSiteRSS("reliefweb.int", 3), type: "disaster" },
  { name: "IMF (site feed)", url: googleNewsSiteRSS("imf.org", 7), type: "economic" },
  { name: "World Bank (site feed)", url: googleNewsSiteRSS("worldbank.org", 7), type: "economic" },
  { name: "Euractiv (site feed)", url: googleNewsSiteRSS("euractiv.com", 7), type: "intl" },
];

function parseArgs(argv) {
  const out = {
    feedsPath: DEFAULT_FEEDS_PATH,
    outPath: "",
    timeoutMs: DEFAULT_TIMEOUT_MS,
    limit: 20,
    includeExpanded: true,
  };
  for (let i = 2; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--feeds" && argv[i + 1]) {
      out.feedsPath = argv[++i];
      continue;
    }
    if (arg === "--out" && argv[i + 1]) {
      out.outPath = argv[++i];
      continue;
    }
    if (arg === "--timeout-ms" && argv[i + 1]) {
      out.timeoutMs = Number(argv[++i]) || DEFAULT_TIMEOUT_MS;
      continue;
    }
    if (arg === "--limit" && argv[i + 1]) {
      out.limit = Number(argv[++i]) || 20;
      continue;
    }
    if (arg === "--include-expanded=false") {
      out.includeExpanded = false;
      continue;
    }
    if (arg === "--include-expanded=true") {
      out.includeExpanded = true;
      continue;
    }
  }
  return out;
}

function normalizeText(input) {
  return String(input || "")
    .replace(/<!\[CDATA\[|\]\]>/g, "")
    .replace(/<[^>]+>/g, " ")
    .replace(/&amp;/g, "&")
    .replace(/&quot;/g, "\"")
    .replace(/&#39;/g, "'")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/\s+/g, " ")
    .trim();
}

function parseIntelFeeds(tsSource) {
  const block = (tsSource.match(feedsBlockRe) || [])[1] || "";
  const out = [];
  let match;
  while ((match = feedEntryRe.exec(block))) {
    out.push({
      name: String(match[1] || "").trim(),
      url: String(match[2] || "").trim(),
      type: String(match[3] || "").trim(),
    });
  }
  return out.filter((f) => f.name && f.url);
}

function mergeFeeds(primary, extra) {
  const out = [];
  const seen = new Set();
  for (const feed of [...primary, ...extra]) {
    const name = String(feed.name || "").trim();
    const url = String(feed.url || "").trim();
    const type = String(feed.type || "").trim();
    if (!name || !url) continue;
    const key = url.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push({ name, url, type });
  }
  return out;
}

function parseTitles(raw, limit = 20) {
  const titles = [];
  const seen = new Set();
  const add = (title) => {
    const normalized = normalizeText(title);
    const key = normalized.toLowerCase();
    if (!normalized || seen.has(key)) {
      return;
    }
    seen.add(key);
    titles.push(normalized);
  };

  const itemRe = /<item\b[\s\S]*?<title\b[^>]*>([\s\S]*?)<\/title>/gi;
  const entryRe = /<entry\b[\s\S]*?<title\b[^>]*>([\s\S]*?)<\/title>/gi;
  let m;
  while ((m = itemRe.exec(raw)) && titles.length < limit) add(m[1]);
  while ((m = entryRe.exec(raw)) && titles.length < limit) add(m[1]);

  if (titles.length === 0) {
    const h2 = [...raw.matchAll(/<h2[^>]*>([\s\S]*?)<\/h2>/gi)];
    const h3 = [...raw.matchAll(/<h3[^>]*>([\s\S]*?)<\/h3>/gi)];
    for (const x of [...h2, ...h3]) {
      add(x[1]);
      if (titles.length >= limit) break;
    }
  }

  return titles;
}

function topTerms(titles, limit = 15) {
  const counts = {};
  for (const title of titles) {
    const tokens = String(title).toLowerCase().match(tokenRe) || [];
    for (const token of tokens) {
      if (stopWords.has(token)) continue;
      counts[token] = (counts[token] || 0) + 1;
    }
  }
  return Object.entries(counts)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, limit);
}

function guessHealth(status, contentType, raw, titles) {
  if (status >= 400) return "error";
  const ct = String(contentType || "").toLowerCase();
  const hasRss = raw.includes("<rss") || raw.includes("<feed");
  if (hasRss || ct.includes("xml") || ct.includes("rss") || ct.includes("atom")) {
    return titles.length > 0 ? "rss_ok" : "rss_empty";
  }
  return titles.length > 0 ? "html_titles" : "non_rss";
}

async function fetchFeed(feed, timeoutMs, limit) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(feed.url, {
      signal: controller.signal,
      redirect: "follow",
      headers: {
        "user-agent": "sanskrite-rss-probe/1.0",
        accept: "application/rss+xml,application/atom+xml,application/xml,text/xml;q=0.9,*/*;q=0.1",
      },
    });
    const raw = await res.text();
    const titles = parseTitles(raw, limit);
    const contentType = res.headers.get("content-type") || "";
    return {
      name: feed.name,
      type: feed.type,
      url: feed.url,
      status: res.status,
      contentType,
      health: guessHealth(res.status, contentType, raw, titles),
      titles,
      topTerms: topTerms(titles),
    };
  } catch (err) {
    return {
      name: feed.name,
      type: feed.type,
      url: feed.url,
      status: 0,
      contentType: "",
      health: "error",
      error: String(err?.message || err),
      titles: [],
      topTerms: [],
    };
  } finally {
    clearTimeout(timer);
  }
}

function recommendFallback(feed) {
  const q = encodeURIComponent(`site:${new URL(feed.url).hostname.replace(/^www\./, "")} when:7d`);
  return `https://news.google.com/rss/search?q=${q}&hl=en-US&gl=US&ceid=US:en`;
}

async function main() {
  const args = parseArgs(process.argv);
  const absFeedsPath = path.resolve(args.feedsPath);
  const ts = fs.readFileSync(absFeedsPath, "utf8");
  let feeds = parseIntelFeeds(ts);
  if (args.includeExpanded) {
    feeds = mergeFeeds(feeds, expandedFeeds);
  }

  const results = [];
  for (const feed of feeds) {
    results.push(await fetchFeed(feed, args.timeoutMs, args.limit));
  }

  const globalCounts = {};
  for (const result of results) {
    for (const [term, count] of result.topTerms || []) {
      globalCounts[term] = (globalCounts[term] || 0) + count;
    }
  }

  const problematic = results
    .filter(
      (r) =>
        r.health === "error" ||
        r.health === "non_rss" ||
        r.health === "html_titles" ||
        r.health === "rss_empty",
    )
    .map((r) => ({
      name: r.name,
      url: r.url,
      status: r.status,
      health: r.health,
      suggestedFallback: recommendFallback(r),
    }));

  const report = {
    generatedAt: new Date().toISOString(),
    feedCount: feeds.length,
    okCount: results.filter((r) => r.health === "rss_ok").length,
    problematicCount: problematic.length,
    topGlobalTerms: Object.entries(globalCounts).sort((a, b) => b[1] - a[1]).slice(0, 100),
    problematic,
    results,
  };

  const output = JSON.stringify(report, null, 2);
  if (args.outPath) {
    const absOut = path.resolve(args.outPath);
    fs.mkdirSync(path.dirname(absOut), { recursive: true });
    fs.writeFileSync(absOut, output + "\n", "utf8");
  }
  process.stdout.write(output + "\n");
}

main().catch((err) => {
  process.stderr.write(`probe failed: ${String(err?.message || err)}\n`);
  process.exit(1);
});
