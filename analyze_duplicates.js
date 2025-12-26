const fs = require('fs');

try {
  const data = JSON.parse(fs.readFileSync('c:/Users/IgorKirisyuk/Downloads/lens-items-LLEPfx1ZKcz3gJqB74rC-2025-12-25.json', 'utf8'));
  const nodes = data.filter(i => i.kind === 'node');

  console.log('--- Label Duplicates ---');
  const labelMap = {};
  nodes.forEach(n => {
    const label = (n.label || '').toLowerCase().trim();
    if (!label) return;
    if (!labelMap[label]) labelMap[label] = [];
    labelMap[label].push(n.canonicalPath);
  });
  
  Object.entries(labelMap).forEach(([label, paths]) => {
    if (paths.length > 1) {
      console.log(`"${label}": ${paths.length} instances`);
      paths.forEach(p => console.log(`  - ${p}`));
    }
  });

  console.log('\n--- Path Suffix Duplicates ---');
  const suffixMap = {};
  nodes.forEach(n => {
    if (!n.canonicalPath) return;
    const parts = n.canonicalPath.split('/');
    const suffix = parts[parts.length - 1].toLowerCase();
    if (suffix.length < 3) return; // skip short suffixes like 'ru', 'na'

    if (!suffixMap[suffix]) suffixMap[suffix] = [];
    suffixMap[suffix].push(n.canonicalPath);
  });

  Object.entries(suffixMap).forEach(([suffix, paths]) => {
    // Filter out identical paths, we want distinct paths with same suffix
    const uniquePaths = [...new Set(paths)];
    if (uniquePaths.length > 1) {
      console.log(`".../${suffix}": ${uniquePaths.length} variants`);
      uniquePaths.forEach(p => console.log(`  - ${p}`));
    }
  });

} catch (err) { console.error(err); }
