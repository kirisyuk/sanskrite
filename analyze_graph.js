const fs = require('fs');

try {
  const data = JSON.parse(fs.readFileSync('c:/Users/IgorKirisyuk/Downloads/lens-items-LLEPfx1ZKcz3gJqB74rC-2025-12-25.json', 'utf8'));
  
  const nodes = data.filter(i => i.kind === 'node');
  const edges = data.filter(i => i.kind === 'edge');
  
  console.log(`Total nodes: ${nodes.length}`);
  console.log(`Total edges: ${edges.length}`);

  const nodeIds = new Set(nodes.map(n => n.fragmentId || n.id));
  // Также добавляем lensItemId для проверки
  const lensItemIds = new Set(nodes.map(n => n.id));

  // 1. Призраки
  const ghosts = edges.filter(e => {
    const s = e.sourceFragmentId;
    const t = e.targetFragmentId;
    return !nodeIds.has(s) || !nodeIds.has(t);
  });
  console.log(`Ghost edges (endpoints missing): ${ghosts.length}`);

  // 2. Петли
  const loops = edges.filter(e => e.sourceFragmentId === e.targetFragmentId);
  console.log(`Self-loops: ${loops.length}`);
  if (loops.length > 0) console.log('Sample loop:', loops[0].sourceFragmentId);

  // 3. Дубликаты рёбер
  const edgeKeys = new Set();
  let duplicates = 0;
  edges.forEach(e => {
    const key = `${e.sourceFragmentId}-${e.targetFragmentId}-${e.edgeType}`;
    if (edgeKeys.has(key)) duplicates++;
    else edgeKeys.add(key);
  });
  console.log(`Exact duplicate edges: ${duplicates}`);

  // 4. Симметричные дубли (для ненаправленных по смыслу связей)
  // Просто считаем пары A-B, где есть и A->B и B->A с одинаковым типом
  const pairMap = new Map();
  edges.forEach(e => {
    const k = `${e.sourceFragmentId}-${e.targetFragmentId}`;
    if (!pairMap.has(k)) pairMap.set(k, new Set());
    pairMap.get(k).add(e.edgeType);
  });
  
  let reverseDupes = 0;
  pairMap.forEach((types, key) => {
    const [s, t] = key.split('-');
    const revKey = `${t}-${s}`;
    if (pairMap.has(revKey)) {
      const revTypes = pairMap.get(revKey);
      types.forEach(type => {
        if (revTypes.has(type)) reverseDupes++;
      });
    }
  });
  // Делим на 2, т.к. посчитали каждую пару дважды
  console.log(`Reverse duplicates (A->B and B->A same type): ${reverseDupes / 2}`);

  // 5. Аномально много связей между парой
  const pairCounts = {};
  edges.forEach(e => {
     const s = e.sourceFragmentId;
     const t = e.targetFragmentId;
     const k = s < t ? `${s}-${t}` : `${t}-${s}`;
     pairCounts[k] = (pairCounts[k] || 0) + 1;
  });
  const heavyPairs = Object.entries(pairCounts).filter(([k, v]) => v > 5);
  console.log(`Pairs with >5 connections: ${heavyPairs.length}`);
  if (heavyPairs.length > 0) {
      console.log('Top 3 heavy pairs:');
      heavyPairs.sort((a,b) => b[1] - a[1]).slice(0, 3).forEach(p => console.log(`  ${p[0]}: ${p[1]} edges`));
  }

} catch (err) {
  console.error(err);
}
