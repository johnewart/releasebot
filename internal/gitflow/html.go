package gitflow

import (
	"encoding/json"
	"io"
	"text/template"
)

type htmlReportData struct {
	ReportJSON string
}

// WriteHTML writes a self-contained D3-based HTML visualization of the status report to w.
// The output file can be opened directly in any browser; D3 is loaded from the CDN.
func WriteHTML(w io.Writer, rep *StatusReport) error {
	jsonBytes, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	tmpl, err := template.New("html").Parse(htmlTmpl)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, htmlReportData{ReportJSON: string(jsonBytes)})
}

// htmlTmpl is the self-contained HTML template. {{.ReportJSON}} is replaced with the JSON payload.
const htmlTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>GitFlow Branch Visualization</title>
<script src="https://d3js.org/d3.v7.min.js"></script>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0d1117; color: #c9d1d9; }
#app { padding: 24px; max-width: 1100px; margin: 0 auto; }
h1 { font-size: 1.35rem; font-weight: 600; color: #f0f6fc; margin-bottom: 3px; }
#subtitle { font-size: 0.82rem; color: #8b949e; margin-bottom: 20px; }
#stats-row { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 20px; }
.stat-card { background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 10px 16px; min-width: 150px; }
.stat-label { font-size: 10px; color: #8b949e; text-transform: uppercase; letter-spacing: 0.6px; }
.stat-value { font-size: 1.5rem; font-weight: 700; color: #f0f6fc; margin-top: 1px; }
.stat-value.ok   { color: #3fb950; }
.stat-value.warn { color: #d29922; }
.stat-value.bad  { color: #f85149; }
.legend { display: flex; gap: 14px; flex-wrap: wrap; align-items: center; margin-bottom: 14px; font-size: 11.5px; color: #8b949e; }
.leg { display: flex; align-items: center; gap: 5px; }
.leg-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
#graph-wrap { background: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow-x: auto; margin-bottom: 20px; }
svg text { user-select: none; }
.lane-odd  { fill: #161b22; }
.lane-even { fill: #13181f; }
.lane-sep  { stroke: #21262d; stroke-width: 1; }
#issues { background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 16px; }
#issues h2 { font-size: 0.95rem; color: #f85149; margin-bottom: 10px; }
.issue { padding: 6px 0; border-bottom: 1px solid #21262d; font-size: 0.82rem; color: #c9d1d9; }
.issue:last-child { border-bottom: none; }
#tip { position: fixed; background: #1c2128; border: 1px solid #30363d; border-radius: 7px;
       padding: 10px 14px; pointer-events: none; font-size: 11.5px; z-index: 200;
       min-width: 190px; display: none; box-shadow: 0 4px 12px #0008; }
#tip-title { font-weight: 600; color: #f0f6fc; margin-bottom: 7px; font-size: 12.5px; }
.tip-row { display: flex; justify-content: space-between; gap: 18px; color: #8b949e; margin: 2px 0; }
.tip-row b { color: #c9d1d9; font-weight: 500; }
.tip-ok   { color: #3fb950 !important; }
.tip-bad  { color: #f85149 !important; }
</style>
</head>
<body>
<div id="app">
  <h1>GitFlow Branch Visualization</h1>
  <div id="subtitle"></div>
  <div id="stats-row"></div>
  <div class="legend">
    <span style="font-size:10px;letter-spacing:.5px;text-transform:uppercase;">Legend</span>
    <div class="leg"><div class="leg-dot" style="background:#58a6ff"></div><span>main</span></div>
    <div class="leg"><div class="leg-dot" style="background:#3fb950"></div><span>develop</span></div>
    <div class="leg"><div class="leg-dot" style="background:#a371f7"></div><span>release (clean)</span></div>
    <div class="leg"><div class="leg-dot" style="background:#d29922"></div><span>ahead of main</span></div>
    <div class="leg"><div class="leg-dot" style="background:#f85149"></div><span>unmerged commits</span></div>
    <div class="leg"><div class="leg-dot" style="background:#ff7b72"></div><span>hotfix</span></div>
  </div>
  <div id="graph-wrap"></div>
  <div id="issues" style="display:none"></div>
</div>
<div id="tip"><div id="tip-title"></div><div id="tip-body"></div></div>

<script>
const R = {{.ReportJSON}};

// ── helpers
function short(sha) { return sha ? sha.substring(0, 7) : '?'; }
function esc(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

// ── subtitle
document.getElementById('subtitle').textContent = 'Remote: ' + (R.remote || 'origin');

// ── stats
const drift = R.integrate_drift || {};
const lines = R.lines || [];
const releaseLines = lines.filter(function(l){ return l.kind === 'release'; });
const hotfixLines  = lines.filter(function(l){ return l.kind === 'hotfix'; });
const unmerged = lines.filter(function(l){ return l.commits_not_in_main > 0; }).length;

var statsRow = document.getElementById('stats-row');
function addStat(label, val, cls) {
  statsRow.innerHTML += '<div class="stat-card"><div class="stat-label">' + label +
    '</div><div class="stat-value ' + (cls||'') + '">' + val + '</div></div>';
}
addStat('Release Branches', releaseLines.length, releaseLines.length ? '' : 'ok');
addStat('Hotfix Branches',  hotfixLines.length,  hotfixLines.length  ? 'warn' : 'ok');
addStat('Main &rarr; Develop', drift.commits_main_not_in_develop || 0,
  (drift.commits_main_not_in_develop||0) > 0 ? 'warn' : 'ok');
addStat('Develop &rarr; Main', drift.commits_develop_not_in_main || 0,
  (drift.commits_develop_not_in_main||0) > 0 ? 'warn' : 'ok');
addStat('Unmerged Branches', unmerged, unmerged > 0 ? 'bad' : 'ok');

// ── issues panel
if ((R.issues||[]).length > 0) {
  var ip = document.getElementById('issues');
  ip.style.display = 'block';
  ip.innerHTML = '<h2>Issues (' + R.issues.length + ')</h2>';
  R.issues.forEach(function(i) {
    ip.innerHTML += '<div class="issue">' + esc(i) + '</div>';
  });
}

// ── layout constants
var LABEL_W  = 190;
var PAD_L    = 16;
var PAD_R    = 60;
var PAD_TOP  = 24;
var LANE_H   = 72;
var NODE_R   = 7;
var SVG_W    = 960;
var GRAPH_L  = LABEL_W + PAD_L;
var GRAPH_R  = SVG_W - PAD_R;
var GRAPH_W  = GRAPH_R - GRAPH_L;

// ── build lane list
var laneDefs = [];
laneDefs.push({ id: 'main',    label: (R.main    && R.main.name)    || 'main',    kind: 'main',    ref: R.main    || {} });
laneDefs.push({ id: 'develop', label: (R.develop && R.develop.name) || 'develop', kind: 'develop', ref: R.develop || {} });
releaseLines.forEach(function(l) { laneDefs.push({ id: l.name, label: l.name, kind: 'release', line: l }); });
hotfixLines.forEach(function(l)  { laneDefs.push({ id: l.name, label: l.name, kind: 'hotfix',  line: l }); });

var SVG_H = laneDefs.length * LANE_H + PAD_TOP * 2;

// lane y-center
function laneY(i) { return PAD_TOP + i * LANE_H + LANE_H / 2; }
var laneIdx = {};
laneDefs.forEach(function(l, i) { laneIdx[l.id] = i; });

// color theme
var COLORS = {
  main:            '#58a6ff',
  develop:         '#3fb950',
  release_clean:   '#a371f7',
  release_warn:    '#d29922',
  release_bad:     '#f85149',
  hotfix_clean:    '#ff7b72',
  hotfix_warn:     '#d29922',
  hotfix_bad:      '#f85149',
};
function lineColor(kind, status) {
  if (kind === 'main')    return COLORS.main;
  if (kind === 'develop') return COLORS.develop;
  return COLORS[kind + '_' + status] || '#8b949e';
}

function lineStatus(line) {
  if (!line) return 'clean';
  if (line.commits_not_in_main > 0) return 'bad';
  if (line.ahead_of_main > 0)       return 'warn';
  return 'clean';
}

// x-scale: map commit count to pixels (log-ish, capped)
function commitPx(n) {
  if (!n || n <= 0) return 0;
  return Math.min(n, 30) / 30 * (GRAPH_W * 0.52) + 20;
}

// ── draw
var svg = d3.select('#graph-wrap')
  .append('svg')
  .attr('width', SVG_W)
  .attr('height', SVG_H);

// arrowhead marker
svg.append('defs').append('marker')
  .attr('id', 'arrow-bad')
  .attr('viewBox', '0 -4 8 8')
  .attr('refX', 7).attr('refY', 0)
  .attr('markerWidth', 5).attr('markerHeight', 5)
  .attr('orient', 'auto')
  .append('path')
  .attr('d', 'M0,-4L8,0L0,4')
  .attr('fill', '#f85149');

svg.append('defs').append('marker')
  .attr('id', 'arrow-warn')
  .attr('viewBox', '0 -4 8 8')
  .attr('refX', 7).attr('refY', 0)
  .attr('markerWidth', 5).attr('markerHeight', 5)
  .attr('orient', 'auto')
  .append('path')
  .attr('d', 'M0,-4L8,0L0,4')
  .attr('fill', '#d29922');

// lane backgrounds and separators
laneDefs.forEach(function(lane, i) {
  svg.append('rect')
    .attr('x', 0).attr('y', PAD_TOP + i * LANE_H)
    .attr('width', SVG_W).attr('height', LANE_H)
    .attr('class', i % 2 === 0 ? 'lane-odd' : 'lane-even');
  if (i > 0) {
    svg.append('line')
      .attr('x1', 0).attr('y1', PAD_TOP + i * LANE_H)
      .attr('x2', SVG_W).attr('y2', PAD_TOP + i * LANE_H)
      .attr('class', 'lane-sep');
  }
});

// vertical divider between labels and graph
svg.append('line')
  .attr('x1', LABEL_W).attr('y1', PAD_TOP)
  .attr('x2', LABEL_W).attr('y2', SVG_H - PAD_TOP)
  .attr('stroke', '#30363d').attr('stroke-width', 1);

// draw main/develop integrate-drift indicator
(function() {
  var mainMissing = R.main && R.main.missing;
  var devMissing  = R.develop && R.develop.missing;
  if (mainMissing || devMissing) return;

  var mIdx = laneIdx['main'];
  var dIdx = laneIdx['develop'];
  if (mIdx === undefined || dIdx === undefined) return;

  var myM = laneY(mIdx);
  var myD = laneY(dIdx);
  var driftX = GRAPH_L + GRAPH_W * 0.22;

  var d2m = drift.commits_main_not_in_develop || 0;
  var m2d = drift.commits_develop_not_in_main || 0;

  if (d2m > 0) {
    // main has commits not in develop: downward arrow main→develop
    svg.append('line')
      .attr('x1', driftX - 12).attr('y1', myM + NODE_R + 2)
      .attr('x2', driftX - 12).attr('y2', myD - NODE_R - 2)
      .attr('stroke', '#d29922').attr('stroke-width', 1.5)
      .attr('stroke-dasharray', '4,3')
      .attr('marker-end', 'url(#arrow-warn)');
    svg.append('text')
      .attr('x', driftX - 18).attr('y', (myM + myD) / 2 + 4)
      .attr('text-anchor', 'end').attr('fill', '#d29922')
      .attr('font-size', '10').text(d2m + ' commit' + (d2m !== 1 ? 's' : ''));
  }
  if (m2d > 0) {
    // develop has commits not in main: upward arrow develop→main
    svg.append('line')
      .attr('x1', driftX + 12).attr('y1', myD - NODE_R - 2)
      .attr('x2', driftX + 12).attr('y2', myM + NODE_R + 2)
      .attr('stroke', '#f85149').attr('stroke-width', 1.5)
      .attr('stroke-dasharray', '4,3')
      .attr('marker-end', 'url(#arrow-bad)');
    svg.append('text')
      .attr('x', driftX + 18).attr('y', (myM + myD) / 2 + 4)
      .attr('text-anchor', 'start').attr('fill', '#f85149')
      .attr('font-size', '10').text(m2d + ' commit' + (m2d !== 1 ? 's' : ''));
  }
})();

// draw each lane's branch line, connectors, and tip node
laneDefs.forEach(function(lane, i) {
  var cy = laneY(i);
  var col, lineX1, lineX2, tipX, forkX;

  if (lane.kind === 'main' || lane.kind === 'develop') {
    col = lineColor(lane.kind, 'clean');
    lineX1 = GRAPH_L;
    lineX2 = GRAPH_R;
    tipX   = GRAPH_R;

    // horizontal branch line
    svg.append('line')
      .attr('x1', lineX1).attr('y1', cy)
      .attr('x2', lineX2 - NODE_R - 1).attr('y2', cy)
      .attr('stroke', col).attr('stroke-width', 3).attr('opacity', 0.85);

    // tip node
    svg.append('circle')
      .attr('cx', tipX).attr('cy', cy).attr('r', NODE_R)
      .attr('fill', col).attr('stroke', '#0d1117').attr('stroke-width', 2)
      .style('cursor', 'pointer')
      .datum(lane)
      .on('mousemove', onHover).on('mouseleave', onLeave);

    // SHA label after tip
    var sha = lane.ref && lane.ref.sha ? lane.ref.sha : '';
    if (sha) {
      svg.append('text')
        .attr('x', tipX + NODE_R + 6).attr('y', cy + 4)
        .attr('fill', '#8b949e').attr('font-size', '10')
        .attr('font-family', 'monospace')
        .text(short(sha));
    }

  } else {
    // release or hotfix
    var line = lane.line;
    var status = lineStatus(line);
    col = lineColor(lane.kind, status);

    var ahead  = line.ahead_of_main  || 0;
    var behind = line.behind_main    || 0;

    // estimate: how far from main tip is the fork?
    // fork was at (behind + ahead) commits ago from main tip
    var totalOffset = commitPx(behind + ahead);
    forkX = Math.max(GRAPH_L + 40, GRAPH_R - totalOffset);
    tipX  = Math.min(GRAPH_R - 16, forkX + commitPx(ahead) + 48);

    // fork curve from parent lane
    var parentId = (lane.kind === 'hotfix') ? 'main' : 'develop';
    var parentIdx2 = laneIdx[parentId];
    var parentY = (parentIdx2 !== undefined) ? laneY(parentIdx2) : laneY(1);

    var curveDir = cy > parentY ? 1 : -1;
    svg.append('path')
      .attr('d',
        'M ' + forkX + ',' + parentY +
        ' C ' + forkX + ',' + (parentY + curveDir * 22) +
        ' '   + forkX + ',' + (cy - curveDir * 22) +
        ' '   + forkX + ',' + cy)
      .attr('fill', 'none')
      .attr('stroke', parentId === 'main' ? COLORS.main : COLORS.develop)
      .attr('stroke-width', 1.5)
      .attr('stroke-dasharray', '3,3')
      .attr('opacity', 0.5);

    // small fork dot on parent lane
    svg.append('circle')
      .attr('cx', forkX).attr('cy', parentY).attr('r', 3.5)
      .attr('fill', parentId === 'main' ? COLORS.main : COLORS.develop)
      .attr('opacity', 0.7);

    // horizontal branch line
    svg.append('line')
      .attr('x1', forkX).attr('y1', cy)
      .attr('x2', tipX - NODE_R - 1).attr('y2', cy)
      .attr('stroke', col).attr('stroke-width', 2.5).attr('opacity', 0.9);

    // if unmerged: dashed "needs merge" arc back to main tip
    if (line.commits_not_in_main > 0) {
      var mainY2 = (laneIdx['main'] !== undefined) ? laneY(laneIdx['main']) : 0;
      var arcCPx = tipX + 40;
      var arcDir = mainY2 < cy ? -1 : 1;
      svg.append('path')
        .attr('d',
          'M ' + tipX + ',' + cy +
          ' Q ' + arcCPx + ',' + (cy + arcDir * 30) +
          ' '   + (GRAPH_R + 4) + ',' + mainY2)
        .attr('fill', 'none')
        .attr('stroke', '#f85149').attr('stroke-width', 1.5)
        .attr('stroke-dasharray', '5,3').attr('opacity', 0.7);
      svg.append('text')
        .attr('x', tipX + 4).attr('y', cy - 11)
        .attr('fill', '#f85149').attr('font-size', '10').attr('font-weight', '600')
        .text(line.commits_not_in_main + ' unmerged');
    }

    // ahead/behind annotation on the branch line
    var midX = (forkX + tipX) / 2;
    if (ahead > 0 || behind > 0) {
      var parts2 = [];
      if (ahead  > 0) parts2.push('+' + ahead);
      if (behind > 0) parts2.push('-' + behind);
      svg.append('text')
        .attr('x', midX).attr('y', cy - 9)
        .attr('text-anchor', 'middle')
        .attr('fill', ahead > 0 ? col : '#8b949e')
        .attr('font-size', '10')
        .text(parts2.join(' / ') + ' vs main');
    }

    // tip node
    svg.append('circle')
      .attr('cx', tipX).attr('cy', cy).attr('r', NODE_R)
      .attr('fill', col).attr('stroke', '#0d1117').attr('stroke-width', 2)
      .style('cursor', 'pointer')
      .datum(lane)
      .on('mousemove', onHover).on('mouseleave', onLeave);

    // SHA label
    var tipSHA = line.tip_sha || '';
    if (tipSHA) {
      svg.append('text')
        .attr('x', tipX + NODE_R + 6).attr('y', cy + 4)
        .attr('fill', '#8b949e').attr('font-size', '10')
        .attr('font-family', 'monospace')
        .text(short(tipSHA));
    }
  }

  // lane label (left column)
  // shorten long names for display
  var displayLabel = lane.label;
  if (displayLabel.length > 24) { displayLabel = displayLabel.substring(0, 22) + '..'; }
  svg.append('text')
    .attr('x', LABEL_W - 12).attr('y', cy - 5)
    .attr('text-anchor', 'end')
    .attr('fill', '#c9d1d9').attr('font-size', '12').attr('font-weight', '600')
    .text(displayLabel);
  svg.append('text')
    .attr('x', LABEL_W - 12).attr('y', cy + 10)
    .attr('text-anchor', 'end')
    .attr('fill', '#8b949e').attr('font-size', '10')
    .attr('letter-spacing', '0.4')
    .text(lane.kind.toUpperCase());
});

// ── tooltip
var tip      = document.getElementById('tip');
var tipTitle = document.getElementById('tip-title');
var tipBody  = document.getElementById('tip-body');

function row(label, val, cls) {
  return '<div class="tip-row"><span>' + label + '</span><b class="' + (cls||'') + '">' + val + '</b></div>';
}

function onHover(event, lane) {
  tipTitle.textContent = lane.label;
  var html = '';
  if (lane.kind === 'main') {
    html += row('SHA', short(lane.ref.sha || ''));
    html += row('Ref used', lane.ref.ref_used || '-');
    html += row('Missing', lane.ref.missing ? 'yes' : 'no');
    var d2 = drift.commits_main_not_in_develop || 0;
    html += row('Commits not in develop', d2, d2 > 0 ? 'tip-bad' : 'tip-ok');
  } else if (lane.kind === 'develop') {
    html += row('SHA', short(lane.ref.sha || ''));
    html += row('Ref used', lane.ref.ref_used || '-');
    var m2 = drift.commits_develop_not_in_main || 0;
    html += row('Commits not in main', m2, m2 > 0 ? 'tip-bad' : 'tip-ok');
  } else {
    var l = lane.line;
    html += row('Kind', l.kind);
    html += row('SHA', short(l.tip_sha || ''));
    html += row('Ahead of main',    l.ahead_of_main    || 0);
    html += row('Behind main',      l.behind_main      || 0, (l.behind_main||0) > 0 ? 'tip-bad' : '');
    html += row('Ahead of develop', l.ahead_of_develop || 0);
    html += row('Behind develop',   l.behind_develop   || 0);
    var nm = l.commits_not_in_main || 0;
    html += row('Unmerged to main', nm, nm > 0 ? 'tip-bad' : 'tip-ok');
  }
  tipBody.innerHTML = html;
  tip.style.display = 'block';
  moveTip(event);
}

function moveTip(event) {
  var x = event.clientX + 14;
  var y = event.clientY - 14;
  if (x + 210 > window.innerWidth)  { x = event.clientX - 224; }
  if (y + 180 > window.innerHeight) { y = event.clientY - 180; }
  tip.style.left = x + 'px';
  tip.style.top  = y + 'px';
}

function onLeave() { tip.style.display = 'none'; }
</script>
</body>
</html>
`
