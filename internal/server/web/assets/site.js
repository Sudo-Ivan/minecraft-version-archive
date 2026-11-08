(function () {
  'use strict';

  var manifestPath = '/mc/game/version_manifest_v2.json';
  var manifestURL = location.origin + manifestPath;

  var urlEls = document.querySelectorAll('[data-manifest-url]');
  urlEls.forEach(function (el) { el.textContent = manifestURL; });

  function flash(btn, text) {
    var label = btn.textContent;
    btn.textContent = text;
    setTimeout(function () { btn.textContent = label; }, 1600);
  }

  function selectFallback() {
    var el = urlEls[0];
    if (!el || !window.getSelection) { return; }
    var sel = window.getSelection();
    sel.removeAllRanges();
    var range = document.createRange();
    range.selectNodeContents(el);
    sel.addRange(range);
  }

  // Copy is delegated so buttons rendered after load (catalog rows, download
  // cards) work without rebinding. Value order: data-copy-value literal,
  // data-copy-target element text, then the manifest URL.
  function copyTextFor(btn) {
    var literal = btn.getAttribute('data-copy-value');
    if (literal) { return literal; }
    var targetSel = btn.getAttribute('data-copy-target');
    if (targetSel) {
      var el = document.querySelector(targetSel);
      if (el) { return el.textContent; }
    }
    return manifestURL;
  }

  document.addEventListener('click', function (ev) {
    var btn = ev.target.closest ? ev.target.closest('[data-copy]') : null;
    if (!btn) { return; }
    var text = copyTextFor(btn);
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(
        function () { flash(btn, 'Copied'); },
        function () { flash(btn, 'Selected'); }
      );
    } else {
      flash(btn, 'Selected');
    }
  });

  function formatBytes(n) {
    if (typeof n !== 'number' || n < 0) { return '—'; }
    var units = ['B', 'KB', 'MB', 'GB', 'TB'];
    var v = n, u = 0;
    while (v >= 1024 && u < units.length - 1) { v /= 1024; u += 1; }
    if (v < 10 && u > 0) { v *= 1024; u -= 1; }
    return (u === 0 ? String(Math.round(v)) : v.toFixed(1)) + ' ' + units[u];
  }

  function formatDate(iso) {
    return typeof iso === 'string' && iso.length >= 10 ? iso.slice(0, 10) : '—';
  }

  function typeClass(t) {
    if (t === 'release') { return 'release'; }
    if (t === 'snapshot') { return 'snapshot'; }
    return 'other';
  }

  function fetchJSON(url) {
    return fetch(url, { headers: { 'Accept': 'application/json' } }).then(function (r) {
      if (!r.ok) { throw new Error('HTTP ' + r.status); }
      return r.json();
    });
  }

  fetch('/p2p/inventory').then(function (r) {
    return r.ok ? r.json() : null;
  }).then(function (inv) {
    if (!inv) { return; }
    if (Array.isArray(inv.versions)) {
      document.querySelectorAll('[data-version-count]').forEach(function (el) {
        el.textContent = String(inv.versions.length);
      });
    }
    if (typeof inv.bytes === 'number' && inv.bytes > 0) {
      var text = ' \u00b7 ' + formatBytes(inv.bytes);
      document.querySelectorAll('[data-archive-size]').forEach(function (el) {
        el.textContent = text;
      });
    }
    // The versions page stats sheet reads the same numbers under its own ids.
    var vb = document.getElementById('stat-bytes');
    if (vb && typeof inv.bytes === 'number') { vb.textContent = formatBytes(inv.bytes); }
  }).catch(function () {});

  var railLinks = Array.prototype.slice.call(document.querySelectorAll('.doc-rail a[href^="#"]'));
  if (railLinks.length && 'IntersectionObserver' in window) {
    var spy = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (!e.isIntersecting) { return; }
        railLinks.forEach(function (l) {
          if (l.getAttribute('href') === '#' + e.target.id) {
            l.setAttribute('aria-current', 'location');
          } else {
            l.removeAttribute('aria-current');
          }
        });
      });
    }, { rootMargin: '-15% 0px -75% 0px' });
    document.querySelectorAll('.doc-body section[id]').forEach(function (s) { spy.observe(s); });
  }

  /* ================= versions browser ================= */

  var verState = null;

  initVersions();

  function initVersions() {
    var listSec = document.getElementById('view-list');
    if (!listSec) { return; }

    verState = {
      versions: [],
      proxyOnMiss: false,
      query: '',
      type: 'all',
      detailId: null
    };
    var m = location.pathname.match(/^\/versions\/([^/]+)$/);
    if (m) { verState.detailId = decodeURIComponent(m[1]); }

    var controls = document.querySelector('.ver-controls');
    if (verState.detailId && controls) { controls.hidden = true; }

    loadCatalog();

    var q = document.getElementById('ver-q');
    var qClear = document.getElementById('ver-q-clear');
    var chips = document.querySelectorAll('.chip[data-type]');
    var debounce = null;

    if (q) {
      q.addEventListener('input', function () {
        verState.query = q.value;
        if (qClear) { qClear.hidden = !q.value; }
        clearTimeout(debounce);
        debounce = setTimeout(renderList, 120);
      });
    }
    if (qClear) {
      qClear.addEventListener('click', function () {
        if (q) { q.value = ''; q.focus(); }
        verState.query = '';
        qClear.hidden = true;
        renderList();
      });
    }
    chips.forEach(function (chip) {
      chip.addEventListener('click', function () {
        chips.forEach(function (c) { c.setAttribute('aria-pressed', 'false'); });
        chip.setAttribute('aria-pressed', 'true');
        verState.type = chip.getAttribute('data-type');
        renderList();
      });
    });
  }

  function loadCatalog() {
    var state = verState;
    fetchJSON('/versions').then(function (doc) {
      state.versions = Array.isArray(doc.versions) ? doc.versions : [];
      state.proxyOnMiss = !!doc.proxy_on_miss;
      fillVersionStats(state);
      if (state.detailId) {
        renderDetail(state.detailId);
      } else {
        renderList();
      }
    }).catch(function () {
      if (state.detailId) {
        renderDetailError(state.detailId);
      } else {
        showCatalogError();
      }
    });
  }

  function fillVersionStats(state) {
    var versions = state.versions;
    var clients = 0, servers = 0;
    versions.forEach(function (v) {
      if (v.client && v.client.archived) { clients += 1; }
      if (v.server && v.server.archived) { servers += 1; }
    });
    setText('stat-versions', String(versions.length));
    setText('stat-client', String(clients));
    setText('stat-server', String(servers));
    var asof = document.getElementById('ver-stats-asof');
    if (asof) { asof.textContent = 'GET /versions'; }
  }

  function setText(id, text) {
    var el = document.getElementById(id);
    if (el) { el.textContent = text; }
  }

  function matches(v, state) {
    if (state.type !== 'all') {
      var cls = typeClass(v.type);
      if (state.type === 'other' ? cls === 'release' || cls === 'snapshot' : cls !== state.type) {
        return false;
      }
    }
    if (state.query) {
      return v.id.toLowerCase().indexOf(state.query.toLowerCase()) !== -1;
    }
    return true;
  }

  function renderList() {
    var state = verState;
    if (!state) { return; }
    var tbody = document.getElementById('catalog-body');
    if (!tbody) { return; }
    tbody.textContent = '';

    var shown = 0;
    var frag = document.createDocumentFragment();
    var rowTpl = document.getElementById('tpl-row');
    var fileTpl = document.getElementById('tpl-file');

    state.versions.forEach(function (v) {
      if (!matches(v, state)) { return; }
      shown += 1;
      frag.appendChild(buildRow(v, rowTpl, fileTpl, state));
    });

    if (shown === 0) {
      var tr = document.createElement('tr');
      var td = document.createElement('td');
      td.colSpan = 8;
      td.appendChild(stateEl('No versions match this filter.', false));
      tr.appendChild(td);
      frag.appendChild(tr);
    }
    tbody.appendChild(frag);

    var count = document.getElementById('ver-count');
    if (count) {
      count.textContent = shown === state.versions.length
        ? shown + (shown === 1 ? ' version' : ' versions')
        : shown + ' of ' + state.versions.length;
    }
  }

  function stateEl(msg, isError) {
    var span = document.createElement('span');
    span.className = 'ver-state' + (isError ? ' is-error' : '');
    span.textContent = msg;
    return span;
  }

  function buildRow(v, rowTpl, fileTpl, state) {
    var tr = rowTpl.content.firstElementChild.cloneNode(true);

    var link = tr.querySelector('.ver-link');
    link.textContent = v.id;
    link.href = v.page || ('/versions/' + encodeURIComponent(v.id));

    var badge = tr.querySelector('.badge');
    badge.textContent = v.type || 'unknown';
    badge.classList.add('badge--' + typeClass(v.type));

    tr.querySelector('.ver-date').textContent = formatDate(v.releaseTime);

    fillFileCell(tr.children[3], v.client, fileTpl, 'client', state);
    fillFileCell(tr.children[4], v.server, fileTpl, 'server', state);

    var assets = tr.querySelector('.ver-assets');
    var ai = v.asset_index;
    if (ai && ai.url && (ai.archived || state.proxyOnMiss)) {
      var a = document.createElement('a');
      a.href = ai.url;
      a.textContent = formatBytes(ai.totalSize);
      a.title = 'Asset index ' + ai.id + (ai.archived ? '' : ' · fetched upstream on first request');
      assets.textContent = '';
      assets.appendChild(a);
    } else if (ai && ai.url) {
      assets.textContent = formatBytes(ai.totalSize) + ' · not archived';
      assets.title = 'Asset index ' + ai.id + ' is not on this mirror';
    } else {
      assets.textContent = '—';
    }

    var java = tr.querySelector('.ver-java');
    if (v.java && v.java.major) {
      var ja = document.createElement('a');
      ja.href = '/java/' + encodeURIComponent(v.id);
      ja.textContent = 'J' + v.java.major;
      ja.title = 'Java ' + v.java.major + (v.java.component ? ' · ' + v.java.component : '');
      java.textContent = '';
      java.appendChild(ja);
    } else {
      java.textContent = '—';
    }

    tr.querySelector('.ver-json').href = v.version_json || '';

    return tr;
  }

  function fillFileCell(td, artifact, fileTpl, kind, state) {
    td.textContent = '';
    if (!artifact || !artifact.url) {
      var none = document.createElement('span');
      none.className = 'dl is-none';
      none.textContent = '—';
      none.title = 'No ' + kind + ' jar is declared for this version';
      td.appendChild(none);
      return;
    }
    if (!artifact.archived && !state.proxyOnMiss) {
      var off = document.createElement('span');
      off.className = 'dl is-none';
      off.textContent = formatBytes(artifact.size);
      off.title = 'Declared by Mojang but not archived on this mirror';
      td.appendChild(off);
      return;
    }
    var node = fileTpl.content.firstElementChild.cloneNode(true);
    node.href = artifact.url;
    node.setAttribute('download', '');
    node.querySelector('.dl__size').textContent = formatBytes(artifact.size);
    var flag = node.querySelector('.dl__flag');
    if (artifact.archived) {
      flag.textContent = 'mirror';
    } else {
      node.classList.add('is-fetch');
      flag.textContent = 'upstream';
      node.title = 'Not archived yet — fetched from Mojang on first request';
    }
    td.appendChild(node);
  }

  function showCatalogError() {
    var tbody = document.getElementById('catalog-body');
    if (!tbody) { return; }
    tbody.textContent = '';
    var tr = document.createElement('tr');
    var td = document.createElement('td');
    td.colSpan = 8;
    var wrap = document.createElement('span');
    wrap.className = 'ver-state is-error';
    wrap.appendChild(document.createTextNode('Could not load the catalog. '));
    var btn = document.createElement('button');
    btn.className = 'btn';
    btn.type = 'button';
    btn.textContent = 'Retry';
    btn.addEventListener('click', function () {
      wrap.textContent = 'Reading the shelf…';
      wrap.className = 'ver-state';
      loadCatalog();
    });
    wrap.appendChild(btn);
    td.appendChild(wrap);
    tr.appendChild(td);
    tbody.appendChild(tr);
    setText('stat-versions', '—');
    setText('stat-client', '—');
    setText('stat-server', '—');
  }

  /* ---- detail view ---- */

  function aliasCandidates(id) {
    var out = [];
    var cur = id;
    while (/\.0$/.test(cur)) {
      cur = cur.slice(0, -2);
      if (!cur) { break; }
      out.push(cur);
    }
    return out;
  }

  function resolveEntry(state, id) {
    var byId = {};
    state.versions.forEach(function (v) { byId[v.id] = v; });
    if (byId[id]) { return byId[id]; }
    var cands = aliasCandidates(id);
    for (var i = 0; i < cands.length; i++) {
      if (byId[cands[i]]) { return byId[cands[i]]; }
    }
    return null;
  }

  function renderDetail(id) {
    var state = verState;
    var entry = resolveEntry(state, id);
    var root = document.getElementById('detail-root');
    var listSec = document.getElementById('view-list');
    var detailSec = document.getElementById('view-detail');
    if (!root || !detailSec) { return; }
    if (listSec) { listSec.hidden = true; }
    detailSec.hidden = false;
    root.textContent = '';

    if (!entry) {
      var miss = document.createElement('div');
      miss.className = 'ver-state is-error';
      miss.textContent = 'Version ' + id + ' is not archived on this mirror.';
      root.appendChild(miss);
      document.title = 'Version not found · minecraft-version-archive';
      return;
    }

    document.title = entry.id + ' · minecraft-version-archive';
    var tpl = document.getElementById('tpl-detail');
    var node = tpl.content.firstElementChild.cloneNode(true);
    root.appendChild(node);

    node.querySelector('.ver-detail__id').textContent = entry.id;

    var badge = node.querySelector('.ver-detail__meta .badge');
    badge.textContent = entry.type || 'unknown';
    badge.classList.add('badge--' + typeClass(entry.type));
    node.querySelector('.ver-detail__date').textContent = formatDate(entry.releaseTime);

    node.querySelector('[data-detail-json]').textContent = location.origin + (entry.version_json || '');
    node.querySelector('[data-detail-page]').href = entry.version_json || '#';

    fillBay(node.querySelector('#detail-bay'), entry, state);
    fillSpec(node.querySelector('#detail-files'), entry, state);
    fillIntegrity(node.querySelector('#detail-integrity'));
  }

  function renderDetailError(id) {
    var root = document.getElementById('detail-root');
    var listSec = document.getElementById('view-list');
    var detailSec = document.getElementById('view-detail');
    if (!root || !detailSec) { return; }
    if (listSec) { listSec.hidden = true; }
    detailSec.hidden = false;
    root.textContent = '';
    var wrap = document.createElement('span');
    wrap.className = 'ver-state is-error';
    wrap.appendChild(document.createTextNode('Could not load the catalog for ' + id + '. '));
    var btn = document.createElement('button');
    btn.className = 'btn';
    btn.type = 'button';
    btn.textContent = 'Retry';
    btn.addEventListener('click', function () { location.reload(); });
    wrap.appendChild(btn);
    root.appendChild(wrap);
  }

  function fillBay(bay, entry, state) {
    if (!bay) { return; }
    bay.textContent = '';
    var cardTpl = document.getElementById('tpl-bay-card');
    addBayCard(bay, cardTpl, 'Client jar', entry.client, 'client', state);
    addBayCard(bay, cardTpl, 'Server jar', entry.server, 'server', state);
  }

  function addBayCard(bay, cardTpl, kind, artifact, kindKey, state) {
    var card = cardTpl.content.firstElementChild.cloneNode(true);
    card.querySelector('.bay__kind').textContent = kind;
    var stateEl = card.querySelector('.bay__state');
    var sizeEl = card.querySelector('.bay__size');
    var hashEl = card.querySelector('.bay__hashval');
    var copyBtn = card.querySelector('.bay__copy');
    var dlBtn = card.querySelector('.bay__btn');
    var noteEl = card.querySelector('.bay__note');

    if (!artifact || !artifact.url) {
      stateEl.textContent = 'not published';
      stateEl.classList.add('is-none');
      sizeEl.textContent = '—';
      hashEl.textContent = '—';
      copyBtn.hidden = true;
      dlBtn.textContent = 'Unavailable';
      dlBtn.removeAttribute('href');
      dlBtn.setAttribute('aria-disabled', 'true');
      noteEl.textContent = 'This version’s JSON declares no ' + kindKey + ' download.';
      card.classList.add('is-empty');
    } else if (artifact.archived) {
      stateEl.textContent = 'on mirror';
      stateEl.classList.add('is-archived');
      sizeEl.textContent = formatBytes(artifact.size);
      hashEl.textContent = artifact.sha1 || '—';
      if (artifact.sha1) { copyBtn.setAttribute('data-copy-value', artifact.sha1); }
      else { copyBtn.hidden = true; }
      dlBtn.textContent = 'Download ' + kindKey + ' jar';
      dlBtn.href = artifact.url;
      dlBtn.setAttribute('download', '');
      noteEl.textContent = 'Bytes are on this mirror; SHA-1 is re-checked on serve.';
    } else if (!state.proxyOnMiss) {
      stateEl.textContent = 'not archived';
      stateEl.classList.add('is-fetch');
      sizeEl.textContent = formatBytes(artifact.size);
      hashEl.textContent = artifact.sha1 || '—';
      if (artifact.sha1) { copyBtn.setAttribute('data-copy-value', artifact.sha1); }
      else { copyBtn.hidden = true; }
      dlBtn.textContent = 'Unavailable';
      dlBtn.removeAttribute('href');
      dlBtn.setAttribute('aria-disabled', 'true');
      noteEl.textContent = 'Declared by Mojang’s version JSON but not on this mirror, and proxy_on_miss is off — the jar URL would 404.';
      card.classList.add('is-empty');
    } else {
      stateEl.textContent = 'upstream fetch';
      stateEl.classList.add('is-fetch');
      sizeEl.textContent = formatBytes(artifact.size);
      hashEl.textContent = artifact.sha1 || '—';
      if (artifact.sha1) { copyBtn.setAttribute('data-copy-value', artifact.sha1); }
      else { copyBtn.hidden = true; }
      dlBtn.textContent = 'Download ' + kindKey + ' jar';
      dlBtn.href = artifact.url;
      dlBtn.setAttribute('download', '');
      noteEl.textContent = 'Not archived yet — the first request fetches it from Mojang and caches the result.';
    }
    bay.appendChild(card);
  }

  function fillSpec(tbody, entry, state) {
    if (!tbody) { return; }
    tbody.textContent = '';
    var tpl = document.getElementById('tpl-spec-row');

    function row(file, detail, href, linkText) {
      var tr = tpl.content.firstElementChild.cloneNode(true);
      tr.querySelector('th').textContent = file;
      tr.querySelectorAll('td')[0].textContent = detail;
      var linkTd = tr.querySelectorAll('td')[1];
      if (href) {
        var a = document.createElement('a');
        a.href = href;
        a.textContent = linkText || href;
        linkTd.textContent = '';
        linkTd.appendChild(a);
      } else {
        linkTd.textContent = '—';
      }
      tbody.appendChild(tr);
    }

    row('version JSON', 'rewritten URLs, Mojang sha1 preserved', entry.version_json, 'open');
    row('manifest', 'held-versions index', '/mc/game/version_manifest_v2.json', 'open');
    if (entry.asset_index && (entry.asset_index.archived || state.proxyOnMiss)) {
      row('asset index ' + entry.asset_index.id,
        formatBytes(entry.asset_index.totalSize) + (entry.asset_index.archived ? ' · on mirror' : ' · fetched upstream on first request'),
        entry.asset_index.url, 'open');
    } else if (entry.asset_index) {
      row('asset index ' + entry.asset_index.id,
        formatBytes(entry.asset_index.totalSize) + ' · not archived', null);
    }
    row('libraries', entry.libraries + (entry.libraries === 1 ? ' artifact' : ' artifacts') + ' under /libraries/', null);
    if (entry.java) {
      var jd = 'Java ' + entry.java.major + (entry.java.component ? ' · ' + entry.java.component : '');
      row('java runtime', jd, '/java/' + encodeURIComponent(entry.id), 'open');
      if (entry.java.temurin) {
        row('temurin jdk', 'Eclipse Temurin ' + entry.java.major, entry.java.temurin, 'open');
      }
    }
    if (entry.fabric_loader) {
      row('fabric loader', 'loader meta archived', entry.fabric_loader, 'open');
    }
    row('p2p inventory', 'peer federation feed', '/p2p/inventory', 'open');
  }

  function fillIntegrity(tbody) {
    if (!tbody) { return; }
    var rows = [
      ['SHA-1 in the version JSON', 'Jar downloads verify against the sha1 values shown above — the same digests Mojang publishes.'],
      ['SHA-1 in asset paths', 'Every asset object URL carries its hash; the mirror re-checks objects on serve.'],
      ['Range requests', 'Jars and indexes accept Range — resume or parallel-fetch large downloads safely.']
    ];
    rows.forEach(function (pair) {
      var div = document.createElement('div');
      var h = document.createElement('h3');
      h.textContent = pair[0];
      var p = document.createElement('p');
      p.textContent = pair[1];
      div.appendChild(h);
      div.appendChild(p);
      tbody.appendChild(div);
    });
  }
})();
