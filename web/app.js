/* MDBox 前端：零依赖（PDF 导出用本地内置的 html2pdf）。 */
(function () {
  'use strict';

  var $ = function (id) { return document.getElementById(id); };

  var state = {
    user: '',
    theme: 'light',
    mode: 'list',      // list | preview | edit
    status: 'active',  // active | archived | all
    category: '',
    tag: '',
    q: '',
    docs: [],
    current: null,
    dirty: false,
    pushed: false      // 当前文档页是否由我们 pushState 进来的（决定「返回」能否直接 history.back）
  };
  var previewTimer = null;
  var shareCtx = null;

  /* ---------- 主题 ---------- */

  function systemTheme() {
    return (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) ? 'dark' : 'light';
  }
  function initTheme() {
    state.theme = localStorage.getItem('mdbox:theme') || systemTheme();
    document.documentElement.setAttribute('data-theme', state.theme);
    updateThemeIcon();
  }
  function updateThemeIcon() {
    var icon = state.theme === 'dark' ? '☀' : '☾';
    ['theme-icon', 'share-theme-icon'].forEach(function (id) {
      var el = $(id);
      if (el) { el.textContent = icon; }
    });
  }
  function toggleTheme() {
    state.theme = state.theme === 'dark' ? 'light' : 'dark';
    localStorage.setItem('mdbox:theme', state.theme);
    document.documentElement.setAttribute('data-theme', state.theme);
    updateThemeIcon();
  }

  /* ---------- 语言 ---------- */

  var refreshLoginMode = null; // 由 bindLogin 注入：切语言时按当前登录/注册态刷新提示

  function refreshTitle() {
    if (shareCtx) {
      document.title = (shareCtx.title || I18N.t('share.title')) + ' · MDBox';
    } else {
      document.title = I18N.t('app.title');
    }
  }

  function updateLangBtn() {
    var label = I18N.lang() === 'zh' ? 'Switch to English' : '切换到中文';
    ['lang-btn', 'login-lang-btn', 'share-lang-btn'].forEach(function (id) {
      var btn = $(id);
      if (!btn) { return; }
      btn.title = label;
      btn.setAttribute('aria-label', label);
    });
  }

  // applyLang 在切换语言后重渲染当前视图；不触碰编辑态输入内容。
  function applyLang() {
    I18N.applyStatic();
    refreshTitle();
    updateLangBtn();
    if (!$('view-login').hidden) {
      if (refreshLoginMode) { refreshLoginMode(); }
      return;
    }
    if (state.user) {
      loadSidebar();
      if (state.mode === 'list') {
        renderListHead();
        renderList();
      } else if (state.mode === 'preview' && state.current) {
        $('d-meta').textContent = metaText(state.current);
      }
    }
  }

  function bindLangButton() {
    ['lang-btn', 'login-lang-btn', 'share-lang-btn'].forEach(function (id) {
      var btn = $(id);
      if (!btn) { return; }
      btn.addEventListener('click', function () {
        I18N.setLang(I18N.lang() === 'zh' ? 'en' : 'zh');
        applyLang();
      });
    });
    updateLangBtn();
  }

  /* ---------- 基础工具 ---------- */

  function api(path, opts) {
    opts = opts || {};
    var init = { method: opts.method || 'GET' };
    if (opts.body) { init.body = opts.body; }
    init.headers = opts.headers || {};
    init.headers['Accept-Language'] = I18N.lang();
    return fetch(path, init).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (res.status === 401) {
          if (!opts.noAuthRedirect) { showLogin(); }
          throw new Error(data.error || I18N.t('error.need_login'));
        }
        if (!res.ok) { throw new Error(data.error || res.statusText); }
        return data;
      });
    });
  }

  function msg(id, text, isError) {
    var el = $(id);
    if (!el) { return; }
    el.textContent = text || '';
    el.style.color = isError ? 'var(--danger)' : '';
    if (text) { setTimeout(function () { if (el.textContent === text) { el.textContent = ''; } }, 2600); }
  }

  function fmtTime(s) {
    if (!s) { return ''; }
    var d = new Date(s);
    if (isNaN(d.getTime())) { return s; }
    var p = function (n) { return n < 10 ? '0' + n : '' + n; };
    return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
  }

  // 列表卡片用的紧凑时间：同年只显示 月-日 时:分，跨年显示 年-月-日。
  function fmtCardTime(s) {
    if (!s) { return ''; }
    var d = new Date(s);
    if (isNaN(d.getTime())) { return s; }
    var p = function (n) { return n < 10 ? '0' + n : '' + n; };
    var md = p(d.getMonth() + 1) + '-' + p(d.getDate());
    if (d.getFullYear() === new Date().getFullYear()) {
      return md + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
    }
    return d.getFullYear() + '-' + md;
  }

  function splitTags(v) {
    return (v || '').split(',').map(function (s) { return s.trim(); }).filter(function (s) { return s; });
  }

  function copyText(text, btn) {
    var done = function () { flash(btn, I18N.t('copied')); };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done).catch(function () { fallbackCopy(text, done); });
    } else {
      fallbackCopy(text, done);
    }
  }
  function fallbackCopy(text, done) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); done(); } catch (e) { /* ignore */ }
    document.body.removeChild(ta);
  }
  function flash(btn, text) {
    var old = btn.textContent;
    btn.textContent = text;
    setTimeout(function () { btn.textContent = old; }, 1200);
  }

  /* ---------- 视图切换 ---------- */

  function showView(name) { // login | share | app
    $('view-login').hidden = name !== 'login';
    $('view-share').hidden = name !== 'share';
    $('app').hidden = name !== 'app';
  }

  // resetSession 清掉上一个用户留在内存与 DOM 里的一切（当前文档、编辑内容、列表、
  // 筛选、搜索、侧栏、MCP token），防止切换账号后看到别人的内容。
  function resetSession() {
    state.user = '';
    state.mode = 'list';
    state.status = 'active';
    state.category = '';
    state.tag = '';
    state.q = '';
    state.docs = [];
    state.current = null;
    state.dirty = false;
    state.pushed = false;
    closeNav();
    clearTimeout(previewTimer);
    $('view-list').hidden = false;
    $('view-doc').hidden = true;
    ['d-preview', 'e-preview', 'list', 'list-head', 'side-categories', 'side-tags', 'mcp-http', 'mcp-stdio'].forEach(function (id) {
      if ($(id)) { $(id).innerHTML = ''; }
    });
    ['d-source', 'e-title', 'e-category', 'e-tags', 'search', 'share-link'].forEach(function (id) {
      if ($(id)) { $(id).value = ''; }
    });
    ['p-title', 'd-meta', 'p-msg', 'e-msg'].forEach(function (id) {
      if ($(id)) { $(id).textContent = ''; }
    });
    ['modal-mcp', 'modal-share', 'modal-dialog'].forEach(function (id) {
      if ($(id)) { $(id).hidden = true; }
    });
    toggleUserMenu(false);
  }

  function showLogin() {
    resetSession();
    if (hashDocId()) { history.replaceState(null, '', listUrl()); }
    showView('login');
    var u = $('login-user');
    if (u) { u.focus(); }
  }

  // renderUser 同时刷新顶栏与抽屉底部的用户名/头像
  function renderUser(user) {
    var initial = (user || 'A').charAt(0);
    $('user-name').textContent = user;
    $('user-avatar').textContent = initial;
    $('side-user-name').textContent = user;
    $('side-avatar').textContent = initial;
  }

  function enterApp(user) {
    state.user = user;
    renderUser(user);
    showView('app');
    refreshTitle();
    loadSidebar();
    if (hashDocId()) { route(); } else { loadList(); }
  }

  /* ---------- 路由：文档页用 #/doc/<id>，让浏览器返回/前进/刷新可用 ---------- */

  function hashDocId() {
    var prefix = '#/doc/';
    if (location.hash.indexOf(prefix) !== 0) { return ''; }
    var id = location.hash.slice(prefix.length).split(/[/?#]/)[0];
    try { return decodeURIComponent(id); } catch (e) { return ''; }
  }

  function listUrl() { return location.pathname + location.search; }

  // 离开文档页回到列表地址：能回退就回退（不留多余历史），否则直接替换当前记录。
  function leaveDocUrl() {
    if (!hashDocId()) { return; }
    if (state.pushed) { state.pushed = false; history.back(); }
    else { history.replaceState(null, '', listUrl()); }
  }

  // 点品牌回文档列表：等价「返回」，但在编辑态有未保存修改时先确认。
  function goHome() {
    if (state.mode === 'edit' && state.dirty && state.current) {
      uiConfirm(I18N.t('doc.discard_confirm'), { title: I18N.t('doc.discard_title'), confirmText: I18N.t('doc.discard_ok'), danger: true })
        .then(function (ok) {
          if (!ok) { return; }
          state.dirty = false;
          showContent('list'); leaveDocUrl(); loadList();
        });
      return;
    }
    showContent('list'); leaveDocUrl(); loadList();
  }

  // 按当前地址显示对应页面：popstate 与首次进入都走这里。
  function route() {
    var id = hashDocId();
    if (id) { openDoc(id); return; }
    showContent('list');
    loadList();
  }

  function onPopState() {
    if (!state.user) { return; }
    if (state.mode === 'edit' && state.dirty && state.current) {
      // 有未保存修改：先把地址栏拉回文档页，确认放弃后再真正后退。
      history.pushState(null, '', '#/doc/' + encodeURIComponent(state.current.id));
      uiConfirm(I18N.t('doc.discard_confirm'), { title: I18N.t('doc.discard_title'), confirmText: I18N.t('doc.discard_ok'), danger: true })
        .then(function (ok) {
          if (!ok) { return; }
          state.dirty = false;
          history.back();
        });
      return;
    }
    route();
  }

  function showContent(mode) { // list | preview | edit
    state.mode = mode;
    var list = mode === 'list';
    $('view-list').hidden = !list;
    $('view-doc').hidden = list;
    if (list) { return; }
    var editing = mode === 'edit';
    $('preview-bar').hidden = editing;
    $('edit-bar').hidden = !editing;
    $('pane-preview').hidden = editing;
    $('pane-edit').hidden = !editing;
  }

  /* ---------- 侧边栏 ---------- */

  /* ---------- 移动端侧栏抽屉 ---------- */
  // 桌面端没有开关按钮，这两个函数只会被窄屏 UI 调用。
  // 相关 CSS 全部写在 @media (max-width:720px) 内，所以不需要在窗口变宽时额外复位。
  function openNav() { document.body.classList.add('nav-open'); }
  function closeNav() { document.body.classList.remove('nav-open'); }

  function loadSidebar() {
    Promise.all([api('/api/categories'), api('/api/tags')]).then(function (res) {
      renderSidebar(res[0].categories || {}, res[1].tags || {});
    }).catch(function () {});
  }

  function sideItem(text, kind, value, count, active) {
    var li = document.createElement('li');
    li.className = 'side-item' + (active ? ' active' : '');
    li.setAttribute('data-kind', kind);
    if (value) { li.setAttribute('data-value', value); }
    li.appendChild(document.createTextNode(text));
    if (typeof count === 'number') {
      var n = document.createElement('span');
      n.className = 'n';
      n.textContent = count;
      li.appendChild(n);
    }
    return li;
  }

  function renderSidebar(cats, tags) {
    var activeNone = state.status === 'active' && !state.category && !state.tag;
    var catEl = $('side-categories');
    catEl.innerHTML = '';
    catEl.appendChild(sideItem(I18N.t('nav.all_docs'), 'all', '', null, activeNone));
    Object.keys(cats).sort(function (a, b) { return cats[b] - cats[a]; }).forEach(function (c) {
      if (!c) { return; }
      catEl.appendChild(sideItem(c, 'category', c, cats[c], state.category === c));
    });

    var tagEl = $('side-tags');
    tagEl.innerHTML = '';
    var keys = Object.keys(tags).sort(function (a, b) { return tags[b] - tags[a]; });
    if (!keys.length) {
      var e = document.createElement('li');
      e.className = 'side-empty';
      e.textContent = I18N.t('nav.no_tags');
      tagEl.appendChild(e);
    }
    keys.forEach(function (t) {
      tagEl.appendChild(sideItem(t, 'tag', t, tags[t], state.tag === t));
    });

    $('side-archived').classList.toggle('active', state.status === 'archived');
  }

  function onSideClick(e) {
    var li = e.target.closest ? e.target.closest('.side-item') : null;
    if (!li) { return; }
    var kind = li.getAttribute('data-kind');
    var v = li.getAttribute('data-value');
    if (kind === 'all') { state.category = ''; state.tag = ''; state.status = 'active'; }
    else if (kind === 'category') { state.category = (state.category === v ? '' : v); state.tag = ''; state.status = 'active'; }
    else if (kind === 'tag') { state.tag = (state.tag === v ? '' : v); state.category = ''; state.status = 'active'; }
    closeNav();
    refresh();
  }

  function refresh() {
    showContent('list');
    leaveDocUrl();
    loadSidebar();
    loadList();
  }

  /* ---------- 列表 ---------- */

  function loadList() {
    var params = new URLSearchParams();
    params.set('status', state.status);
    if (state.q) { params.set('q', state.q); }
    if (state.tag) { params.set('tag', state.tag); }
    if (state.category) { params.set('category', state.category); }
    return api('/api/docs?' + params.toString()).then(function (data) {
      state.docs = data.docs || [];
      renderListHead();
      renderList();
    }).catch(function () {});
  }

  function renderListHead() {
    var bits = [];
    if (state.status === 'archived') { bits.push(I18N.t('list.status_archived')); }
    if (state.category) { bits.push(I18N.t('list.filter_category', { v: state.category })); }
    if (state.tag) { bits.push(I18N.t('list.filter_tag', { v: state.tag })); }
    if (state.q) { bits.push(I18N.t('list.filter_search', { v: state.q })); }
    $('list-head').textContent = (bits.length ? bits.join(' · ') + ' · ' : '') + I18N.t('list.count', { n: state.docs.length });
  }

  function renderList() {
    var list = $('list');
    list.innerHTML = '';
    $('empty').hidden = state.docs.length > 0;
    state.docs.forEach(function (d) {
      var card = document.createElement('div');
      card.className = 'card';
      card.onclick = function () { openDoc(d.id); };

      var t = document.createElement('div');
      t.className = 't';
      t.textContent = d.title || d.id;
      card.appendChild(t);

      // 删除按钮：默认隐藏，鼠标悬停卡片时出现
      var del = document.createElement('button');
      del.type = 'button';
      del.className = 'card-del';
      del.title = I18N.t('list.delete_title');
      del.setAttribute('aria-label', I18N.t('list.delete_aria'));
      del.innerHTML = '<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' +
        '<path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/></svg>';
      del.onclick = function (e) { e.stopPropagation(); deleteDoc(d.id, d.title || d.id); };
      card.appendChild(del);

      if (d.tags && d.tags.length) {
        var tags = document.createElement('div');
        tags.className = 'tags';
        d.tags.forEach(function (x) {
          var s = document.createElement('span');
          s.textContent = x;
          tags.appendChild(s);
        });
        card.appendChild(tags);
      }

      var m = document.createElement('div');
      m.className = 'm';
      var left = document.createElement('span');
      left.className = 'm-left';
      var bits = [fmtCardTime(d.updated)];
      if (d.category) { bits.push(d.category); }
      left.textContent = bits.join(' · ');
      m.appendChild(left);
      if (d.shared) {
        var sh = document.createElement('span');
        sh.className = 'm-shared';
        sh.title = I18N.t('list.shared');
        sh.setAttribute('aria-label', I18N.t('list.shared'));
        sh.innerHTML = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' +
          '<path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.72"/>' +
          '<path d="M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>';
        m.appendChild(sh);
      }
      card.appendChild(m);

      list.appendChild(card);
    });
  }

  /* ---------- 文档：预览 / 编辑 ---------- */

  function metaText(d) {
    var bits = [d.id, fmtTime(d.created), fmtTime(d.updated)];
    if (d.shared) { bits.push(I18N.t('doc.shared_on')); }
    return bits.filter(Boolean).join(' · ');
  }

  function openDoc(id) {
    return api('/api/docs/' + id).then(function (data) {
      state.current = data.doc;
      state.current.shareUrl = data.shareUrl || '';
      state.dirty = false;
      $('d-preview').innerHTML = data.html || '';
      $('p-title').textContent = data.doc.title || data.doc.id;
      $('d-meta').textContent = metaText(data.doc);
      $('p-share').classList.toggle('primary', !!data.doc.shared);
      if (hashDocId() !== id) {
        history.pushState(null, '', '#/doc/' + encodeURIComponent(id));
        state.pushed = true;
      }
      showContent('preview');
    }).catch(function (e) {
      showContent('list');
      if (hashDocId()) { history.replaceState(null, '', listUrl()); state.pushed = false; }
      loadList();
      if (state.user) { uiAlert(e.message || I18N.t('doc.open_failed'), { title: I18N.t('doc.open_failed') }); }
    });
  }

  function enterEdit() {
    if (!state.current) { return; }
    $('e-title').value = state.current.title || '';
    $('e-category').value = state.current.category || '';
    $('e-tags').value = (state.current.tags || []).join(', ');
    $('d-source').value = state.current.content || '';
    $('e-preview').innerHTML = $('d-preview').innerHTML;
    state.dirty = false;
    showContent('edit');
    $('d-source').focus();
  }

  function cancelEdit() {
    if (!state.dirty) { showContent('preview'); return; }
    uiConfirm(I18N.t('doc.discard_confirm'), { title: I18N.t('doc.discard_title'), confirmText: I18N.t('doc.discard_ok'), danger: true })
      .then(function (ok) {
        if (!ok) { return; }
        state.dirty = false;
        showContent('preview');
      });
  }

  function saveDoc() {
    if (!state.current) { return Promise.resolve(); }
    var id = state.current.id;
    var body = JSON.stringify({
      title: $('e-title').value,
      content: $('d-source').value,
      tags: splitTags($('e-tags').value),
      category: $('e-category').value
    });
    msg('e-msg', I18N.t('doc.saving'));
    return api('/api/docs/' + id, { method: 'PUT', body: body, headers: { 'Content-Type': 'application/json' } })
      .then(function () { state.dirty = false; return openDoc(id); })
      .catch(function (e) { msg('e-msg', e.message, true); });
  }

  // deleteDoc 彻底删除（不可恢复），与 archiveDoc 的软删除不同。
  function deleteDoc(id, title) {
    uiConfirm(I18N.t('doc.delete_confirm', { title: title }),
      { title: I18N.t('doc.delete_title'), confirmText: I18N.t('action.delete'), danger: true })
      .then(function (ok) {
        if (!ok) { return; }
        api('/api/docs/' + id, { method: 'DELETE' })
          .then(function () { loadSidebar(); loadList(); })
          .catch(function (e) { uiAlert(I18N.t('doc.delete_failed', { msg: e.message }), { title: I18N.t('doc.delete_failed_title') }); });
      });
  }

  function createDoc() {
    var body = JSON.stringify({ title: I18N.t('doc.untitled'), content: '# ' + I18N.t('doc.untitled') + '\n\n', source: 'web' });
    return api('/api/docs', { method: 'POST', body: body, headers: { 'Content-Type': 'application/json' } })
      .then(function (data) { loadSidebar(); return openDoc(data.doc.id).then(enterEdit); })
      .catch(function (e) { msg('p-msg', e.message, true); });
  }

  function archiveDoc() {
    if (!state.current) { return; }
    uiConfirm(I18N.t('doc.archive_confirm'), { title: I18N.t('doc.archive_title'), confirmText: I18N.t('action.archive') })
      .then(function (ok) {
        if (!ok) { return; }
        api('/api/docs/' + state.current.id + '/archive', { method: 'POST' })
          .then(function () { state.current = null; state.dirty = false; refresh(); })
          .catch(function (e) { msg('e-msg', e.message, true); });
      });
  }

  /* ---------- 下载 / 导出 ---------- */

  function downloadMd() {
    if (!state.current) { return; }
    var id = state.current.id, title = state.current.title || 'doc';
    fetch('/api/docs/' + id + '/download', { headers: { 'Accept-Language': I18N.lang() } })
      .then(function (r) {
        if (!r.ok) { throw new Error(I18N.t('share.download_failed')); }
        return r.blob();
      })
      .then(function (blob) {
        var a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = title.replace(/[\\/:*?"<>|]/g, '_') + '.md';
        a.click();
        setTimeout(function () { URL.revokeObjectURL(a.href); }, 1000);
      })
      .catch(function (e) { msg('p-msg', e.message, true); });
  }

  // 按需加载 html2pdf：首页不再预加载这个 ~900KB 的包，只有点「导出 PDF」时才拉取。
  var pdfLibPromise = null;
  function loadPdfLib() {
    if (typeof window.html2pdf === 'function') { return Promise.resolve(); }
    if (pdfLibPromise) { return pdfLibPromise; }
    pdfLibPromise = new Promise(function (resolve, reject) {
      var s = document.createElement('script');
      s.src = '/vendor/html2pdf.bundle.min.js';
      s.onload = function () { resolve(); };
      s.onerror = function () { pdfLibPromise = null; reject(new Error(I18N.t('pdf.load_failed'))); };
      document.head.appendChild(s);
    });
    return pdfLibPromise;
  }

  // exportPdf 把某个渲染元素的 HTML 导出为 PDF，返回一个 Promise。
  function exportPdf(el, title) {
    return loadPdfLib().then(function () {
      var wrap = document.createElement('div');
      // 同时带上 markdown 类：表格边框、代码、引用等样式才会生效
      wrap.className = 'pdf-export markdown';
      wrap.innerHTML = el.innerHTML;
      document.body.appendChild(wrap);
      var name = (title || 'doc').replace(/[\\/:*?"<>|]/g, '_');
      function cleanup() {
        if (wrap.parentNode) { wrap.parentNode.removeChild(wrap); }
      }
      return window.html2pdf().set({
        margin: [10, 10, 10, 10],
        filename: name + '.pdf',
        image: { type: 'jpeg', quality: 0.98 },
        html2canvas: { scale: 2, useCORS: true, backgroundColor: '#ffffff' },
        jsPDF: { unit: 'mm', format: 'a4', orientation: 'portrait' },
        // 避免整块内容被分页拦腰切断：css 模式读 break-inside，
        // legacy 模式按下列选择器把元素整体推到下一页。
        pagebreak: {
          mode: ['css', 'legacy'],
          avoid: ['h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'p', 'li', 'blockquote', 'pre', 'tr', 'img']
        }
      }).from(wrap).save().then(cleanup, function (e) { cleanup(); throw e; });
    });
  }

  function downloadPdf() {
    if (!state.current) { return; }
    msg('p-msg', I18N.t('pdf.generating'));
    exportPdf($('d-preview'), state.current.title)
      .catch(function (e) { msg('p-msg', e.message, true); });
  }

  /* ---------- 分享 ---------- */

  function openShare() {
    if (!state.current) { return; }
    var shared = !!state.current.shared;
    $('share-toggle').checked = shared;
    updateShareLink(shared);
    openModal('modal-share');
  }

  function updateShareLink(shared) {
    if (shared && state.current && state.current.shareUrl) {
      $('share-link').value = location.origin + state.current.shareUrl;
      $('share-link-wrap').hidden = false;
    } else {
      $('share-link-wrap').hidden = true;
    }
  }

  function onToggleShare() {
    if (!state.current) { return; }
    var on = $('share-toggle').checked;
    api('/api/docs/' + state.current.id + '/share', {
      method: 'POST',
      body: JSON.stringify({ shared: on }),
      headers: { 'Content-Type': 'application/json' }
    }).then(function (resp) {
      state.current.shared = resp.shared;
      state.current.shareUrl = resp.url || '';
      $('p-share').classList.toggle('primary', resp.shared);
      $('d-meta').textContent = metaText(state.current);
      updateShareLink(resp.shared);
      loadList();
    }).catch(function (e) {
      $('share-toggle').checked = !on;
      uiAlert(I18N.t('error.op_failed', { msg: e.message }), { title: I18N.t('error.op_failed_title') });
    });
  }

  /* ---------- 弹窗 ---------- */

  function openModal(id) { $(id).hidden = false; }
  function closeModal(id) { $(id).hidden = true; }

  /* ---------- 自定义对话框（替代原生 confirm/alert） ---------- */

  var dlgResolve = null;

  function bindDialog() {
    $('dlg-ok').addEventListener('click', function () { closeDialog(true); });
    $('dlg-cancel').addEventListener('click', function () { closeDialog(false); });
  }

  function closeDialog(result) {
    if ($('modal-dialog').hidden) { return; }
    $('modal-dialog').hidden = true;
    var r = dlgResolve;
    dlgResolve = null;
    if (r) { r(result); }
  }

  // openDialog 返回 Promise：确认 true / 取消(含 Esc、点遮罩、点×) false
  function openDialog(opts) {
    opts = opts || {};
    $('dlg-title').textContent = opts.title || I18N.t('action.dialog_title');
    $('dlg-msg').textContent = opts.message || '';
    $('dlg-ok').textContent = opts.confirmText || I18N.t('action.ok');
    $('dlg-cancel').textContent = opts.cancelText || I18N.t('action.cancel');
    $('dlg-cancel').hidden = !!opts.alertOnly;
    $('dlg-ok').className = 'primary' + (opts.danger ? ' danger' : '');
    openModal('modal-dialog');
    setTimeout(function () { $('dlg-ok').focus(); }, 0);
    return new Promise(function (resolve) { dlgResolve = resolve; });
  }

  function uiConfirm(message, opts) {
    opts = opts || {};
    opts.message = message;
    return openDialog(opts);
  }

  function uiAlert(message, opts) {
    opts = opts || {};
    opts.message = message;
    opts.alertOnly = true;
    if (!opts.confirmText) { opts.confirmText = I18N.t('action.got_it'); }
    return openDialog(opts);
  }

  function renderMcp(token) {
    var http = {
      mcpServers: {
        mdbox: {
          url: location.origin + '/mcp',
          headers: { Authorization: 'Bearer ' + token }
        }
      }
    };
    var stdio = {
      mcpServers: {
        mdbox: {
          command: '/path/to/mdbox',
          args: ['-stdio', '-user', state.user, '-data', '/path/to/data']
        }
      }
    };
    $('mcp-http').textContent = JSON.stringify(http, null, 2);
    $('mcp-stdio').textContent = JSON.stringify(stdio, null, 2);
  }

  // 顶栏账号下拉菜单
  function toggleUserMenu(force) {
    var menu = $('user-menu');
    var open = (typeof force === 'boolean') ? force : menu.hidden;
    menu.hidden = !open;
    $('user-btn').setAttribute('aria-expanded', open ? 'true' : 'false');
  }

  function openMcp() {
    api('/api/me').then(function (d) {
      renderMcp(d.token);
      openModal('modal-mcp');
    }).catch(function (err) { uiAlert(err.message || I18N.t('mcp.token_failed')); });
  }

  function resetMcpToken() {
    uiConfirm(I18N.t('mcp.reset_confirm')).then(function (ok) {
      if (!ok) { return; }
      api('/api/me/token', { method: 'POST' }).then(function (d) {
        renderMcp(d.token);
      }).catch(function (err) { uiAlert(err.message || I18N.t('mcp.reset_failed')); });
    });
  }

  function openPwd() {
    $('pwd-form').reset();
    $('pwd-msg').textContent = '';
    openModal('modal-pwd');
    $('pwd-old').focus();
  }

  function submitPwd(e) {
    e.preventDefault();
    var oldPwd = $('pwd-old').value, newPwd = $('pwd-new').value;
    if (newPwd !== $('pwd-new2').value) { msg('pwd-msg', I18N.t('pwd.mismatch'), true); return; }
    api('/api/me/password', {
      method: 'POST',
      body: JSON.stringify({ old: oldPwd, new: newPwd }),
      headers: { 'Content-Type': 'application/json' }
    }).then(function () {
      closeModal('modal-pwd');
      uiAlert(I18N.t('pwd.changed'));
    }).catch(function (err) { msg('pwd-msg', err.message || I18N.t('pwd.failed'), true); });
  }

  function doLogout() {
    api('/api/logout', { method: 'POST', noAuthRedirect: true }).catch(function () {}).then(showLogin);
  }

  /* ---------- 上传 ---------- */

  function uploadFiles(files) {
    if (!files || !files.length) { return; }
    var fd = new FormData();
    Array.prototype.forEach.call(files, function (f) { fd.append('files', f); });
    fetch('/api/upload', { method: 'POST', body: fd, headers: { 'Accept-Language': I18N.lang() } })
      .then(function (r) { return r.json(); })
      .then(function (d) {
        if (d && d.error) { throw new Error(d.error); }
        loadSidebar();
        loadList();
      })
      .catch(function (e) { uiAlert(I18N.t('upload.failed', { msg: e.message }), { title: I18N.t('upload.failed_title') }); });
  }

  /* ---------- 事件绑定 ---------- */

  function bindEvents() {
    bindDialog();
    $('side-toggle').addEventListener('click', function () {
      if (document.body.classList.contains('nav-open')) { closeNav(); } else { openNav(); }
    });
    $('side-backdrop').addEventListener('click', closeNav);
    $('theme-btn').addEventListener('click', toggleTheme);
    $('mcp-btn').addEventListener('click', openMcp);
    $('mcp-reset').addEventListener('click', resetMcpToken);
    $('user-btn').addEventListener('click', function (e) {
      e.stopPropagation();
      toggleUserMenu();
    });
    $('pwd-btn').addEventListener('click', function () { toggleUserMenu(false); openPwd(); });
    $('pwd-form').addEventListener('submit', submitPwd);
    $('logout-btn').addEventListener('click', function () { toggleUserMenu(false); doLogout(); });
    // 窄屏：同样的操作放在抽屉底部，先收起抽屉再动作，避免弹窗盖在打开的抽屉上
    $('side-mcp').addEventListener('click', function () { closeNav(); openMcp(); });
    $('side-pwd').addEventListener('click', function () { closeNav(); openPwd(); });
    $('side-logout').addEventListener('click', function () { closeNav(); doLogout(); });

    $('side-categories').addEventListener('click', onSideClick);
    $('side-tags').addEventListener('click', onSideClick);
    $('side-archived').addEventListener('click', function () {
      state.status = 'archived'; state.category = ''; state.tag = '';
      closeNav();
      refresh();
    });

    var refreshTimer = null;
    $('search').addEventListener('input', function () {
      clearTimeout(refreshTimer);
      var v = this.value;
      refreshTimer = setTimeout(function () { state.q = v.trim(); loadList(); }, 250);
    });
    $('btn-new').addEventListener('click', createDoc);
    $('btn-upload').addEventListener('click', function () { $('file-input').click(); });
    $('file-input').addEventListener('change', function () { uploadFiles(this.files); this.value = ''; });

    $('p-back').addEventListener('click', goHome);
    $('app-brand').addEventListener('click', goHome);
    $('p-edit').addEventListener('click', enterEdit);
    $('p-share').addEventListener('click', openShare);
    $('p-download').addEventListener('click', function (e) {
      e.stopPropagation();
      $('download-menu').hidden = !$('download-menu').hidden;
    });
    $('dl-md').addEventListener('click', function () { $('download-menu').hidden = true; downloadMd(); });
    $('dl-pdf').addEventListener('click', function () { $('download-menu').hidden = true; downloadPdf(); });

    $('e-cancel').addEventListener('click', cancelEdit);
    $('e-save').addEventListener('click', saveDoc);
    $('e-archive').addEventListener('click', archiveDoc);
    $('share-toggle').addEventListener('change', onToggleShare);

    ['e-title', 'e-category', 'e-tags'].forEach(function (id) {
      $(id).addEventListener('input', function () { state.dirty = true; });
    });

    $('d-source').addEventListener('input', function () {
      state.dirty = true;
      clearTimeout(previewTimer);
      var v = this.value;
      previewTimer = setTimeout(function () {
        api('/api/preview', { method: 'POST', body: JSON.stringify({ content: v }), headers: { 'Content-Type': 'application/json' } })
          .then(function (r) { $('e-preview').innerHTML = r.html || ''; })
          .catch(function () {});
      }, 300);
    });

    // 弹窗关闭
    document.addEventListener('click', function (e) {
      var closer = e.target.closest && e.target.closest('[data-close]');
      if (closer) {
        var mid = closer.getAttribute('data-close');
        if (mid === 'modal-dialog') { closeDialog(false); } else { closeModal(mid); }
        return;
      }
      if (e.target.classList && e.target.classList.contains('modal')) {
        if (e.target.id === 'modal-dialog') { closeDialog(false); } else { e.target.hidden = true; }
        return;
      }
      var cp = e.target.closest && e.target.closest('[data-copy]');
      if (cp) { copyText($(cp.getAttribute('data-copy')).textContent, cp); return; }
      var ci = e.target.closest && e.target.closest('[data-copy-input]');
      if (ci) { copyText($(ci.getAttribute('data-copy-input')).value, ci); return; }
      // 点击空白收起下载菜单 / 账号菜单
      var menu = $('download-menu');
      if (menu && !menu.hidden) { menu.hidden = true; }
      toggleUserMenu(false);
    });

    window.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        if (!$('user-menu').hidden) { toggleUserMenu(false); return; }
        if (!$('modal-dialog').hidden) { closeDialog(false); return; }
        var modalIds = ['modal-mcp', 'modal-share'];
        var openModalId = modalIds.filter(function (id) { return !$(id).hidden; })[0];
        if (openModalId) { $(openModalId).hidden = true; return; }
        closeNav();
        return;
      }
      if (e.key === 'Enter' && !$('modal-dialog').hidden) { closeDialog(true); return; }
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault();
        if (state.mode === 'edit') { saveDoc(); }
      }
    });

    document.addEventListener('dragover', function (e) { e.preventDefault(); });
    document.addEventListener('drop', function (e) {
      if (!e.dataTransfer || !e.dataTransfer.files.length) { return; }
      e.preventDefault();
      uploadFiles(e.dataTransfer.files);
    });

    window.addEventListener('beforeunload', function (e) {
      if (state.mode === 'edit' && state.dirty) { e.preventDefault(); e.returnValue = ''; }
    });
  }

  function bindLogin() {
    var registering = false;
    function setMode(reg) {
      registering = reg;
      $('login-tip').textContent = reg ? I18N.t('login.tip_register') : I18N.t('login.tip');
      $('login-pass2').hidden = !reg;
      $('login-pass').autocomplete = reg ? 'new-password' : 'current-password';
      $('login-submit').textContent = reg ? I18N.t('login.register') : I18N.t('login.submit');
      $('login-switch').textContent = reg ? I18N.t('login.to_login') : I18N.t('login.to_register');
      $('login-msg').textContent = '';
    }
    refreshLoginMode = function () { setMode(registering); };
    $('login-switch').addEventListener('click', function () { setMode(!registering); });
    $('login-form').addEventListener('submit', function (e) {
      e.preventDefault();
      $('login-msg').textContent = '';
      if (registering && $('login-pass').value !== $('login-pass2').value) {
        $('login-msg').textContent = I18N.t('auth.register_mismatch');
        return;
      }
      api(registering ? '/api/register' : '/api/login', {
        method: 'POST',
        body: JSON.stringify({ username: $('login-user').value, password: $('login-pass').value }),
        headers: { 'Content-Type': 'application/json' },
        noAuthRedirect: true
      }).then(function (d) {
        $('login-pass').value = '';
        $('login-pass2').value = '';
        setMode(false);
        enterApp(d.user);
      }).catch(function (err) {
        $('login-msg').textContent = err.message || (registering ? I18N.t('auth.register_failed') : I18N.t('auth.login_failed'));
      });
    });
  }

  /* ---------- 分享模式（匿名只读） ---------- */

  function setupShare() {
    showView('share');
    bindShareEvents();
    var parts = location.pathname.split('/').filter(Boolean); // ['s', user, id, sig]
    var content = $('share-content');
    if (parts.length < 4) { content.innerHTML = '<p>' + I18N.t('share.invalid') + '</p>'; return; }
    shareCtx = { user: parts[1], id: parts[2], sig: parts[3], title: '' };
    fetch(shareApi(), { headers: { 'Accept-Language': I18N.lang() } })
      .then(function (r) { return r.json().then(function (d) { return { ok: r.ok, data: d }; }); })
      .then(function (res) {
        if (!res.ok || res.data.error) { content.innerHTML = '<p>' + I18N.t('share.gone') + '</p>'; return; }
        shareCtx.title = res.data.doc.title || '';
        content.innerHTML = res.data.html || '';
        refreshTitle();
      })
      .catch(function () { content.innerHTML = '<p>' + I18N.t('share.load_failed') + '</p>'; });
  }

  function shareApi() {
    return '/api/share/' + encodeURIComponent(shareCtx.user) + '/' + encodeURIComponent(shareCtx.id) + '/' + encodeURIComponent(shareCtx.sig);
  }

  function bindShareEvents() {
    bindDialog();
    $('share-theme-btn').addEventListener('click', toggleTheme);
    $('share-brand').addEventListener('click', function () { location.href = '/'; });
    $('share-clone').addEventListener('click', cloneShared);
    $('share-download').addEventListener('click', function (e) {
      e.stopPropagation();
      $('share-download-menu').hidden = !$('share-download-menu').hidden;
    });
    $('share-dl-md').addEventListener('click', function () { $('share-download-menu').hidden = true; shareDownloadMd(); });
    $('share-dl-pdf').addEventListener('click', function () { $('share-download-menu').hidden = true; shareDownloadPdf(); });
    document.addEventListener('click', function () {
      var m = $('share-download-menu');
      if (m && !m.hidden) { m.hidden = true; }
    });
  }

  function shareDownloadMd() {
    if (!shareCtx) { return; }
    fetch(shareApi() + '/download', { headers: { 'Accept-Language': I18N.lang() } })
      .then(function (r) { if (!r.ok) { throw new Error(I18N.t('share.download_failed')); } return r.blob(); })
      .then(function (blob) {
        var a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = (shareCtx.title || 'doc').replace(/[\\/:*?"<>|]/g, '_') + '.md';
        a.click();
        setTimeout(function () { URL.revokeObjectURL(a.href); }, 1000);
      }).catch(function () {});
  }

  function shareDownloadPdf() {
    exportPdf($('share-content'), shareCtx ? shareCtx.title : 'doc').catch(function (e) {
      uiAlert(e.message || I18N.t('pdf.export_failed'), { title: I18N.t('pdf.export_failed_title') });
    });
  }

  // 复制当前分享的文档到自己登录账号的库里，成功后打开副本。
  function cloneShared() {
    if (!shareCtx) { return; }
    fetch('/api/clone/' + encodeURIComponent(shareCtx.user) + '/' +
          encodeURIComponent(shareCtx.id) + '/' + encodeURIComponent(shareCtx.sig), {
      method: 'POST',
      headers: { 'Accept-Language': I18N.lang() }
    }).then(function (r) {
      if (r.status === 401) { location.href = '/'; return null; } // 未登录 → 跳登录页
      if (!r.ok) { throw new Error(I18N.t('share.clone_failed')); }
      return r.json();
    }).then(function (data) {
      if (!data || !data.doc) { return; }
      location.href = '/#/doc/' + encodeURIComponent(data.doc.id); // 进自己的库并打开副本
    }).catch(function (e) {
      uiAlert(e.message || I18N.t('share.clone_failed'), { title: I18N.t('action.dialog_title') });
    });
  }

  /* ---------- 启动 ---------- */

  function boot() {
    initTheme();
    I18N.applyStatic();
    bindLangButton();
    if (location.pathname.indexOf('/s/') === 0) { setupShare(); return; }
    bindEvents();
    bindLogin();
    refreshTitle();
    window.addEventListener('popstate', onPopState);
    api('/api/me').then(function (d) { enterApp(d.user); }).catch(showLogin);
  }

  boot();
})();
