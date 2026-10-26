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

  document.querySelectorAll('[data-copy]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(manifestURL).then(
          function () { flash(btn, 'Copied'); },
          function () { selectFallback(); flash(btn, 'Selected'); }
        );
      } else {
        selectFallback();
        flash(btn, 'Selected');
      }
    });
  });

  fetch('/p2p/inventory').then(function (r) {
    return r.ok ? r.json() : null;
  }).then(function (inv) {
    if (!inv || !Array.isArray(inv.versions)) { return; }
    document.querySelectorAll('[data-version-count]').forEach(function (el) {
      el.textContent = String(inv.versions.length);
    });
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
})();
