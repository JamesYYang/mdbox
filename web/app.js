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
    overview: null,   // 列表页「近 7 天」概览；切语言时用它重渲染，不重复请求
    current: null,
    dirty: false,
    pushed: false,     // 当前文档页是否由我们 pushState 进来的（决定「返回」能否直接 history.back）
    allTags: [],       // 库内已有标签，供胶囊输入做建议
    editTags: []       // 编辑态当前已选标签
  };
  var previewTimer = null;
  var shareCtx = null; // { user, id, sig, title, mode, content, editing, draft }
  var sharePreviewTimer = null;
  var imgUploads = 0; // 进行中的图片上传队列数；保存前必须为 0，否则插入的内容会丢

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
    if (shareCtx) { updateShareChrome(); return; }
    if (state.user) {
      loadSidebar();
      if (state.mode === 'list') {
        renderListHead();
        renderList();
        renderOverview();
      } else if (state.mode === 'preview' && state.current) {
        renderDocMeta(state.current);
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
    if (opts.signal) { init.signal = opts.signal; }
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

  /* ---------- 图片：粘贴 / 拖拽 / 选择后上传，按光标位置插入 ---------- */

  // clipboardImages 取剪贴板里的图片文件；Chrome/Firefox 走 files，Safari 走 items。
  function clipboardImages(e) {
    var dt = e.clipboardData;
    if (!dt) { return []; }
    if (dt.files && dt.files.length) { return Array.prototype.slice.call(dt.files); }
    var out = [];
    Array.prototype.forEach.call(dt.items || [], function (it) {
      if (it.kind === 'file') { var f = it.getAsFile(); if (f) { out.push(f); } }
    });
    return out;
  }

  function altOf(f) {
    var n = (f.name || '').replace(/\.[^.]+$/, '').replace(/[\[\]]/g, ' ').trim();
    return n || I18N.t('img.alt');
  }

  // 编辑器已关掉（textarea 被 hidden 或被父级收起）时不要再写值：
  // 上传可能在保存/取消之后才完成，写进隐藏的 textarea 会污染下一次 enterEdit 的初值。
  function insertAtCursor(ta, text) {
    if (!ta || ta.offsetParent === null) { return; }
    var start = ta.selectionStart, end = ta.selectionEnd, v = ta.value;
    ta.value = v.slice(0, start) + text + v.slice(end);
    var pos = start + text.length;
    ta.setSelectionRange(pos, pos);
    ta.dispatchEvent(new Event('input', { bubbles: true }));
  }

  // insertImages 串行上传，逐张按选择顺序插入；单张失败不中断其余。
  // imgUploads 在队列开始时 +1、结束时 -1，保存逻辑据此拒绝在图片还没落地时提交。
  // onStart 可选：队列开始前调用一次，分享页用它在可能触发登录跳转前把正文存起来。
  function insertImages(ta, files, msgId, onStart) {
    var list = Array.prototype.filter.call(files || [], function (f) { return /^image\//.test(f.type); });
    if (!list.length) { return; }
    var failed = 0, lastErr = '';
    if (onStart) { onStart(); }
    imgUploads++;
    function step(i) {
      if (i >= list.length) {
        imgUploads--;
        // 只失败一张时直接说原因（服务端已本地化，如「图片过大，单张上限 5MB」），
        // 多张失败时给数量 + 最后一条原因。
        if (failed === 1 && lastErr) { msg(msgId, lastErr, true); }
        else if (failed) {
          msg(msgId, lastErr
            ? I18N.t('img.upload_failed_reason', { n: failed, reason: lastErr })
            : I18N.t('img.upload_failed', { n: failed }), true);
        } else { msg(msgId, ''); }
        return;
      }
      var f = list[i];
      msg(msgId, I18N.t('img.uploading', { done: i + 1, total: list.length }));
      var fd = new FormData();
      fd.append('file', f, f.name || 'image');
      // 单张 60 秒超时：半开连接会让 promise 永远 pending，imgUploads 不归零就再也存不了档。
      var ctrl = new AbortController();
      var timer = setTimeout(function () { ctrl.abort(); }, 60000);
      api('/api/assets', { method: 'POST', body: fd, signal: ctrl.signal }).then(function (d) {
        clearTimeout(timer);
        insertAtCursor(ta, '![' + altOf(f) + '](' + d.url + ')\n');
        step(i + 1);
      }).catch(function (e) {
        clearTimeout(timer);
        failed++;
        // 中止给的是浏览器自带的英文报错，不外露；交给队列结束时的「N 张失败」文案。
        lastErr = (e && e.name === 'AbortError') ? '' : ((e && e.message) || '');
        step(i + 1);
      });
    }
    step(0);
  }

  // bindImageInput 给一个 textarea 接上粘贴与拖拽；按钮单独绑。onStart 原样转给 insertImages。
  function bindImageInput(ta, msgId, onStart) {
    ta.addEventListener('paste', function (e) {
      var files = clipboardImages(e);
      if (!files.length) { return; }
      e.preventDefault();
      insertImages(ta, files, msgId, onStart);
    });
    ta.addEventListener('dragover', function (e) {
      if (e.dataTransfer && e.dataTransfer.types && Array.prototype.indexOf.call(e.dataTransfer.types, 'Files') >= 0) {
        e.preventDefault();
      }
    });
    ta.addEventListener('drop', function (e) {
      if (!e.dataTransfer || !e.dataTransfer.files || !e.dataTransfer.files.length) { return; }
      // 只有拖进来的文件里有图片才由编辑器接管；否则放行，交给全局监听器按文档上传。
      var hasImage = Array.prototype.some.call(e.dataTransfer.files, function (f) {
        return /^image\//.test((f && f.type) || '');
      });
      if (!hasImage) { return; }
      e.preventDefault();
      e.stopPropagation();
      insertImages(ta, e.dataTransfer.files, msgId, onStart);
    });
  }

  // 紧凑时间（列表卡片与详情页 meta）：同年 MM-DD HH:MM，跨年 YYYY-MM-DD。
  function fmtShortTime(s) {
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

  /* ---------- 标签胶囊输入（可选可输） ---------- */
  // 已选标签放在 state.editTags，#e-tags 只承载「还没加入」的那一个。
  function renderTagChips() {
    var box = $('e-tags-chips');
    if (!box) { return; }
    box.innerHTML = '';
    state.editTags.forEach(function (t, i) {
      var chip = document.createElement('span');
      chip.className = 'tag-chip';
      var label = document.createElement('span');
      label.textContent = t;
      var del = document.createElement('button');
      del.type = 'button';
      del.textContent = '×';
      del.title = I18N.t('edit.tag_remove');
      del.addEventListener('click', function () { removeTag(i); });
      chip.appendChild(label);
      chip.appendChild(del);
      box.appendChild(chip);
    });
  }

  // 支持一次粘贴多个（逗号/顿号分隔）；重复标签按大小写不敏感去重。
  function addTag(raw) {
    var added = false;
    (raw || '').split(/[,，]/).forEach(function (p) {
      var t = p.replace(/\s+/g, ' ').trim();
      if (!t) { return; }
      var dup = state.editTags.some(function (x) { return x.toLowerCase() === t.toLowerCase(); });
      if (!dup) { state.editTags.push(t); added = true; }
    });
    if (added) { state.dirty = true; }
    $('e-tags').value = '';
    hideTagSuggest();
    renderTagChips();
  }

  // 把输入框里还没按回车的那一段也收进胶囊，保存前调用。
  function commitPendingTag() {
    addTag($('e-tags').value);
  }

  function removeTag(i) {
    if (i < 0 || i >= state.editTags.length) { return; }
    state.editTags.splice(i, 1);
    state.dirty = true;
    renderTagChips();
  }

  function hideTagSuggest() {
    var box = $('tag-suggest');
    if (box) { box.hidden = true; box.innerHTML = ''; }
  }

  // 建议只列库内已有标签，排除已选，最多 8 条。
  function showTagSuggest() {
    var box = $('tag-suggest');
    if (!box) { return; }
    var q = ($('e-tags').value || '').trim().toLowerCase();
    var list = state.allTags.filter(function (t) {
      if (state.editTags.indexOf(t) >= 0) { return false; }
      return !q || t.toLowerCase().indexOf(q) >= 0;
    }).slice(0, 8);
    if (!list.length) { hideTagSuggest(); return; }
    box.innerHTML = '';
    list.forEach(function (t) {
      var b = document.createElement('button');
      b.type = 'button';
      b.textContent = t;
      // 用 mousedown 而不是 click：click 之前输入框已经 blur，下拉先消失了
      b.addEventListener('mousedown', function (e) { e.preventDefault(); addTag(t); });
      box.appendChild(b);
    });
    box.hidden = false;
  }

  function bindTagInput() {
    var input = $('e-tags');
    if (!input) { return; }
    input.addEventListener('input', function () { state.dirty = true; showTagSuggest(); });
    input.addEventListener('focus', showTagSuggest);
    input.addEventListener('blur', function () { setTimeout(hideTagSuggest, 120); });
    input.addEventListener('keydown', function (e) {
      if (e.isComposing) { return; } // 中文输入法组词时的回车不算提交
      if (e.key === 'Enter' || e.key === ',' || e.key === '，') {
        e.preventDefault();
        var v = input.value.trim();
        if (v) { addTag(v); return; }
        // 输入框为空时回车 = 选中第一条建议
        var first = $('tag-suggest').hidden ? null : $('tag-suggest').querySelector('button');
        if (first) { addTag(first.textContent); } else { hideTagSuggest(); }
        return;
      }
      if (e.key === 'Backspace' && !input.value) { removeTag(state.editTags.length - 1); return; }
      if (e.key === 'Escape') { hideTagSuggest(); }
    });
    var wrap = $('e-tags-wrap');
    if (wrap) {
      wrap.addEventListener('click', function (e) {
        if (e.target === wrap || e.target.id === 'e-tags-chips') { input.focus(); }
      });
    }
  }

  function loadAllTags() {
    api('/api/tags')
      .then(function (r) { state.allTags = r.tags || []; })
      .catch(function () { state.allTags = []; });
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
    state.overview = null;
    var ov = $('overview');
    if (ov) { ov.hidden = true; }
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
    clearToc($('doc-toc'));
    ['d-source', 'e-title', 'e-category', 'e-tags', 'search', 'share-link'].forEach(function (id) {
      if ($(id)) { $(id).value = ''; }
    });
    state.allTags = [];
    state.editTags = [];
    if ($('e-tags-chips')) { $('e-tags-chips').innerHTML = ''; }
    hideTagSuggest();
    ['p-title', 'doc-meta', 'p-msg', 'e-msg'].forEach(function (id) {
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
    // 回到列表（首次进入或用户后退）：此时不该再认为自己有一个「可回退的文档历史」。
    state.pushed = false;
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
    $('doc-meta').hidden = editing;
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
    loadOverview();
    return api('/api/docs?' + params.toString()).then(function (data) {
      state.docs = data.docs || [];
      renderListHead();
      renderList();
    }).catch(function () {});
  }

  // 概览是全局统计（不受筛选影响），因此独立请求；失败时静默隐藏，不干扰列表。
  function loadOverview() {
    return api('/api/overview?days=7').then(function (d) {
      state.overview = d;
      renderOverview();
    }).catch(function () {});
  }

  function renderOverview() {
    var el = $('overview');
    var d = state.overview;
    var total = d ? (d['new'] || 0) + (d.updated || 0) + (d.archived || 0) : 0;
    if (!total) { el.hidden = true; return; }

    el.innerHTML = '';
    var range = document.createElement('span');
    range.className = 'overview-range';
    range.innerHTML = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
      '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>';
    range.appendChild(document.createTextNode(I18N.t('overview.range', { days: d.days })));
    el.appendChild(range);

    // 每个指标是「小标签 + 大数字」，中英文都不存在词序问题。
    function stat(labelKey, n, cls) {
      var box = document.createElement('span');
      box.className = 'overview-stat' + (cls ? ' ' + cls : '');
      var label = document.createElement('span');
      label.className = 'overview-label';
      label.textContent = I18N.t(labelKey);
      var num = document.createElement('b');
      num.textContent = n;
      box.appendChild(label);
      box.appendChild(num);
      el.appendChild(box);
    }
    stat('overview.label_new', d['new']);
    stat('overview.label_updated', d.updated);
    stat('overview.label_archived', d.archived);
    if (d.agent > 0) { stat('overview.label_agent', d.agent, 'agent'); }
    el.hidden = false;
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

      // 「⋯」菜单：归档/恢复 + 删除。默认隐藏，悬停卡片或键盘聚焦时出现。
      var wrap = document.createElement('div');
      wrap.className = 'card-actions dropdown';
      wrap.addEventListener('click', function (e) { e.stopPropagation(); });

      var more = document.createElement('button');
      more.type = 'button';
      more.className = 'card-more';
      more.title = I18N.t('action.more_aria');
      more.setAttribute('aria-label', I18N.t('action.more_aria'));
      more.setAttribute('aria-haspopup', 'menu');
      more.innerHTML = '<svg viewBox="0 0 24 24" width="15" height="15" fill="currentColor" aria-hidden="true">' +
        '<circle cx="5" cy="12" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="19" cy="12" r="1.7"/></svg>';
      wrap.appendChild(more);

      var menu = document.createElement('div');
      menu.className = 'dropdown-menu card-menu';
      menu.hidden = true;
      var archived = d.status === 'archived';
      menu.appendChild(menuButton(archived ? 'action.restore' : 'action.archive', function () {
        closeMenus();
        if (archived) { unarchiveDoc(d.id); } else { archiveDoc(d.id); }
      }));
      menu.appendChild(menuButton('action.delete', function () {
        closeMenus();
        deleteDoc(d.id, d.title || d.id);
      }, true));
      wrap.appendChild(menu);

      more.addEventListener('click', function (e) {
        e.stopPropagation();
        var wasOpen = !menu.hidden;
        closeMenus();
        if (!wasOpen) {
          menu.hidden = false;
          wrap.classList.add('open');
          more.setAttribute('aria-expanded', 'true');
        }
      });
      card.appendChild(wrap);

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
      var bits = [fmtShortTime(d.updated)];
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

  // 详情页顶部 meta：分类（📁 chip）+ 标签 chips + 来源 + 更新时间 + 分享图标。
  function renderDocMeta(d) {
    var box = $('doc-meta');
    box.innerHTML = '';
    if (d.category) {
      var cat = document.createElement('span');
      cat.className = 'dm-cat';
      cat.innerHTML = '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' +
        '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>';
      var ct = document.createElement('span');
      ct.textContent = d.category;
      cat.appendChild(ct);
      box.appendChild(cat);
    }
    (d.tags || []).forEach(function (x) {
      var tag = document.createElement('span');
      tag.className = 'dm-tag';
      tag.textContent = x;
      box.appendChild(tag);
    });
    var spacer = document.createElement('span');
    spacer.className = 'spacer';
    box.appendChild(spacer);
    if (d.source) {
      var src = document.createElement('span');
      src.className = 'dm-txt';
      src.textContent = I18N.t('doc.source') + ' ' + d.source;
      box.appendChild(src);
    }
    var up = document.createElement('span');
    up.className = 'dm-txt';
    up.textContent = I18N.t('doc.updated') + ' ' + fmtShortTime(d.updated);
    box.appendChild(up);
    if (d.shared) {
      var sh = document.createElement('span');
      sh.className = 'dm-shared';
      sh.title = I18N.t('list.shared');
      sh.setAttribute('aria-label', I18N.t('list.shared'));
      sh.innerHTML = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' +
        '<path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.72"/>' +
        '<path d="M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>';
      box.appendChild(sh);
    }
  }

  /* ---------- 右侧目录（只收 h1/h2） ---------- */
  // 不依赖标题 id：bluemonday 的 id 白名单只认 ASCII，中文标题的 id 会被清掉。
  // 所以这里直接持有标题元素，点击时 scrollIntoView，也避开和 #/doc/ 哈希路由抢锚点。
  function clearToc(nav) {
    if (nav._tocOff) { nav._tocOff(); nav._tocOff = null; }
    nav.innerHTML = '';
    nav.hidden = true;
  }

  function buildToc(article, nav, scroller) {
    clearToc(nav);
    var heads = article.querySelectorAll('h1, h2');
    if (heads.length < 2) { return; } // 只有标题没有分节的文档，目录没有意义

    // 文档可能只用了 ##，此时不缩进；有 # 时才把 ## 缩进一级。
    var minLevel = 2;
    Array.prototype.forEach.call(heads, function (h) {
      if (h.tagName === 'H1') { minLevel = 1; }
    });

    var title = document.createElement('div');
    title.className = 'toc-title';
    title.setAttribute('data-i18n', 'toc.title');
    title.textContent = I18N.t('toc.title');

    var list = document.createElement('div');
    list.className = 'toc-list';
    var items = [];
    Array.prototype.forEach.call(heads, function (h) {
      var lvl = h.tagName === 'H1' ? 1 : 2;
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'toc-item' + (lvl > minLevel ? ' lvl2' : '');
      btn.textContent = h.textContent.trim();
      btn.title = btn.textContent;
      btn.addEventListener('click', function () {
        h.scrollIntoView({ behavior: 'smooth', block: 'start' });
      });
      list.appendChild(btn);
      items.push({ btn: btn, head: h });
    });

    nav.appendChild(title);
    nav.appendChild(list);
    nav.hidden = false;

    // 滚动高亮：取越过视口上沿 80px 的最后一个标题。
    var ticking = false;
    function sync() {
      ticking = false;
      var line = scroller.getBoundingClientRect().top + 80;
      var at = 0;
      for (var i = 0; i < items.length; i++) {
        if (items[i].head.getBoundingClientRect().top <= line) { at = i; }
      }
      // 最后一节太短时滚不到顶部，直接按「已到底」高亮最后一条。
      // 内容不够长（没有可滚动距离）时不能这样判定，否则一打开就高亮最后一条。
      if (scroller.scrollHeight > scroller.clientHeight + 4 &&
          scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 4) {
        at = items.length - 1;
      }
      for (var j = 0; j < items.length; j++) {
        items[j].btn.classList.toggle('active', j === at);
      }
    }
    function onScroll() {
      if (ticking) { return; }
      ticking = true;
      requestAnimationFrame(sync);
    }
    scroller.addEventListener('scroll', onScroll, { passive: true });
    nav._tocOff = function () { scroller.removeEventListener('scroll', onScroll); };
    sync();
  }

  function openDoc(id) {
    return api('/api/docs/' + id).then(function (data) {
      state.current = data.doc;
      state.current.shareUrl = data.shareUrl || '';
      state.dirty = false;
      $('d-preview').innerHTML = data.html || '';
      $('p-title').textContent = data.doc.title || data.doc.id;
      renderDocMeta(data.doc);
      $('p-share').classList.toggle('primary', !!data.doc.shared);
      var isArchived = data.doc.status === 'archived';
      $('doc-archive').hidden = isArchived;
      $('doc-restore').hidden = !isArchived;
      $('doc-menu').hidden = true;
      if (hashDocId() !== id) {
        history.pushState(null, '', '#/doc/' + encodeURIComponent(id));
        state.pushed = true;
      }
      showContent('preview');
      $('pane-preview').scrollTop = 0;
      buildToc($('d-preview'), $('doc-toc'), $('pane-preview'));
    }).catch(function (e) {
      showContent('list');
      clearToc($('doc-toc'));
      if (hashDocId()) { history.replaceState(null, '', listUrl()); state.pushed = false; }
      loadList();
      if (state.user) { uiAlert(e.message || I18N.t('doc.open_failed'), { title: I18N.t('doc.open_failed') }); }
    });
  }

  function enterEdit() {
    if (!state.current) { return; }
    $('e-title').value = state.current.title || '';
    $('e-category').value = state.current.category || '';
    state.editTags = (state.current.tags || []).slice();
    $('e-tags').value = '';
    renderTagChips();
    hideTagSuggest();
    loadAllTags();
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
    // 图片还在上传时保存会把未落地的图片一起丢掉（保存后回到预览态，插入也就无处可去）。
    if (imgUploads > 0) { msg('e-msg', I18N.t('img.uploading_wait'), true); return Promise.resolve(); }
    var id = state.current.id;
    commitPendingTag(); // 输入框里没按回车的那一段也要存
    var body = JSON.stringify({
      title: $('e-title').value,
      content: $('d-source').value,
      tags: state.editTags.slice(),
      category: $('e-category').value
    });
    msg('e-msg', I18N.t('doc.saving'));
    return api('/api/docs/' + id, { method: 'PUT', body: body, headers: { 'Content-Type': 'application/json' } })
      .then(function () { state.dirty = false; return openDoc(id); })
      .catch(function (e) { msg('e-msg', e.message, true); });
  }

  // 关闭所有「⋯」菜单。列表卡片菜单与文档页菜单共用，保证同时只开一个。
  function closeMenus() {
    var menus = document.querySelectorAll('.dropdown-menu');
    Array.prototype.forEach.call(menus, function (m) { m.hidden = true; });
    var wraps = document.querySelectorAll('.card-actions.open');
    Array.prototype.forEach.call(wraps, function (w) { w.classList.remove('open'); });
    var triggers = document.querySelectorAll('[aria-haspopup]');
    Array.prototype.forEach.call(triggers, function (t) { t.setAttribute('aria-expanded', 'false'); });
  }

  // menuButton 生成一个菜单项，danger 为真时用红字（删除这类不可恢复的动作）。
  function menuButton(key, onClick, danger) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = danger ? 'menu-danger' : '';
    b.textContent = I18N.t(key);
    b.addEventListener('click', function (e) { e.stopPropagation(); closeMenus(); onClick(); });
    return b;
  }

  // deleteDoc 彻底删除（不可恢复），与 archiveDoc 的软删除不同。
  function deleteDoc(id, title) {
    uiConfirm(I18N.t('doc.delete_confirm', { title: title }),
      { title: I18N.t('doc.delete_title'), confirmText: I18N.t('action.delete'), danger: true })
      .then(function (ok) {
        if (!ok) { return; }
        api('/api/docs/' + id, { method: 'DELETE' })
          .then(function () { state.current = null; state.dirty = false; refresh(); })
          .catch(function (e) { uiAlert(I18N.t('doc.delete_failed', { msg: e.message }), { title: I18N.t('doc.delete_failed_title') }); });
      });
  }

  function createDoc() {
    var body = JSON.stringify({ title: I18N.t('doc.untitled'), content: '# ' + I18N.t('doc.untitled') + '\n\n', source: 'web' });
    return api('/api/docs', { method: 'POST', body: body, headers: { 'Content-Type': 'application/json' } })
      .then(function (data) { loadSidebar(); return openDoc(data.doc.id).then(enterEdit); })
      .catch(function (e) { msg('p-msg', e.message, true); });
  }

  // archiveDoc 归档（软删除）。传参用于列表卡片，不传则取当前打开的文档。
  function archiveDoc(id) {
    if (!id && state.current) { id = state.current.id; }
    if (!id) { return; }
    uiConfirm(I18N.t('doc.archive_confirm'), { title: I18N.t('doc.archive_title'), confirmText: I18N.t('action.archive') })
      .then(function (ok) {
        if (!ok) { return; }
        api('/api/docs/' + id + '/archive', { method: 'POST' })
          .then(function () { state.current = null; state.dirty = false; refresh(); })
          .catch(function (e) { uiAlert(I18N.t('doc.archive_failed', { msg: e.message }), { title: I18N.t('doc.archive_title') }); });
      });
  }

  // unarchiveDoc 恢复（Archive 的逆操作）。成功后切回活动列表，用户能看到它重新出现。
  function unarchiveDoc(id) {
    if (!id && state.current) { id = state.current.id; }
    if (!id) { return; }
    api('/api/docs/' + id + '/unarchive', { method: 'POST' })
      .then(function () {
        state.current = null;
        state.dirty = false;
        // 恢复到活动区后切回活动列表，用户能直接看到它回到列表里。
        state.status = 'active';
        refresh();
      })
      .catch(function (e) { uiAlert(I18N.t('doc.restore_failed', { msg: e.message }), { title: I18N.t('doc.restore_title') }); });
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

  // waitImages 等容器内的图片加载完再交给 html2canvas，否则本地图会导出成空白。
  // 用超时兜底，避免某张图挂住整个导出。
  function waitImages(root, timeoutMs) {
    var imgs = Array.prototype.slice.call(root.querySelectorAll('img'));
    if (!imgs.length) { return Promise.resolve(); }
    var pending = imgs.map(function (img) {
      if (img.complete) { return Promise.resolve(); }
      return new Promise(function (resolve) {
        img.addEventListener('load', resolve, { once: true });
        img.addEventListener('error', resolve, { once: true });
      });
    });
    return Promise.race([
      Promise.all(pending),
      new Promise(function (resolve) { setTimeout(resolve, timeoutMs || 5000); })
    ]);
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
      // 图片是本站 /a/ 地址，html2canvas 抓画布前必须先加载完。
      return waitImages(wrap, 5000).then(function () {
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
        }).from(wrap).save();
      }).then(cleanup, function (e) { cleanup(); throw e; });
    });
  }

  function downloadPdf() {
    if (!state.current) { return; }
    msg('p-msg', I18N.t('pdf.generating'));
    exportPdf($('d-preview'), state.current.title)
      .catch(function (e) { msg('p-msg', e.message, true); });
  }

  /* ---------- 分享 ---------- */

  var SHARE_HINTS = {
    off: 'sharemodal.hint_off',
    view: 'sharemodal.hint_view',
    edit: 'sharemodal.hint_edit'
  };

  function openShare() {
    if (!state.current) { return; }
    $('share-mode').value = state.current.share_mode || 'off';
    updateShareModeHint();
    updateShareLink($('share-mode').value);
    openModal('modal-share');
  }

  function updateShareModeHint() {
    var m = $('share-mode').value;
    var text = I18N.t(SHARE_HINTS[m] || SHARE_HINTS.off);
    // 归档期间链接是 404，但模式仍可改（恢复后按当前模式生效），所以只提示不阻拦。
    if (m !== 'off' && state.current && state.current.status === 'archived') {
      text += ' ' + I18N.t('share.archived_hint');
    }
    $('share-mode-hint').textContent = text;
  }

  function updateShareLink(mode) {
    if (mode !== 'off' && state.current && state.current.shareUrl) {
      $('share-link').value = location.origin + state.current.shareUrl;
      $('share-link-wrap').hidden = false;
    } else {
      $('share-link-wrap').hidden = true;
    }
  }

  function onChangeShareMode() {
    if (!state.current) { return; }
    var mode = $('share-mode').value;
    var prev = state.current.share_mode || 'off';
    updateShareModeHint();
    api('/api/docs/' + state.current.id + '/share', {
      method: 'POST',
      body: JSON.stringify({ mode: mode }),
      headers: { 'Content-Type': 'application/json' }
    }).then(function (resp) {
      state.current.share_mode = resp.mode;
      state.current.shared = resp.mode !== 'off';
      state.current.shareUrl = resp.url || '';
      $('p-share').classList.toggle('primary', resp.mode !== 'off');
      renderDocMeta(state.current);
      updateShareLink(resp.mode);
      loadList();
    }).catch(function (e) {
      $('share-mode').value = prev;
      updateShareModeHint();
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
    if (open) { closeMenus(); }
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
    $('doc-more').addEventListener('click', function (e) {
      e.stopPropagation();
      var wasOpen = !$('doc-menu').hidden;
      closeMenus();
      $('doc-menu').hidden = wasOpen;
      $('doc-more').setAttribute('aria-expanded', wasOpen ? 'false' : 'true');
    });
    $('doc-archive').addEventListener('click', function () { closeMenus(); archiveDoc(); });
    $('doc-restore').addEventListener('click', function () { closeMenus(); unarchiveDoc(); });
    $('doc-delete').addEventListener('click', function () {
      closeMenus();
      if (state.current) { deleteDoc(state.current.id, state.current.title || state.current.id); }
    });
    $('p-download').addEventListener('click', function (e) {
      e.stopPropagation();
      var wasOpen = !$('download-menu').hidden;
      closeMenus();
      $('download-menu').hidden = wasOpen;
    });
    $('dl-md').addEventListener('click', function () { $('download-menu').hidden = true; downloadMd(); });
    $('dl-pdf').addEventListener('click', function () { $('download-menu').hidden = true; downloadPdf(); });

    $('e-cancel').addEventListener('click', cancelEdit);
    $('e-save').addEventListener('click', saveDoc);
    bindImageInput($('d-source'), 'e-msg');
    $('e-img').addEventListener('click', function () { $('e-img-file').click(); });
    $('e-img-file').addEventListener('change', function () {
      insertImages($('d-source'), this.files, 'e-msg');
      this.value = '';
    });
    $('share-mode').addEventListener('change', onChangeShareMode);

    ['e-title', 'e-category'].forEach(function (id) {
      $(id).addEventListener('input', function () { state.dirty = true; });
    });
    bindTagInput();

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
      closeMenus();
      toggleUserMenu(false);
    });

    window.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        if (document.querySelector('.dropdown-menu:not([hidden])')) { closeMenus(); toggleUserMenu(false); return; }
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
        if (location.pathname.indexOf('/s/') === 0) { resumeShare(d.user); }
        else { enterApp(d.user); }
      }).catch(function (err) {
        $('login-msg').textContent = err.message || (registering ? I18N.t('auth.register_failed') : I18N.t('auth.login_failed'));
      });
    });
  }

  /* ---------- 分享模式（匿名只读） ---------- */

  // 登录成功后回到分享页：重取文档，并把草稿填回编辑态。
  function resumeShare(user) {
    state.user = user;
    renderUser(user);
    setupShare();
  }

  function setupShare() {
    showView('share');
    var parts = location.pathname.split('/').filter(Boolean); // ['s', user, id, sig]
    var content = $('share-content');
    if (parts.length < 4) {
      exitShareEdit(); // 上一条链接可能还停在编辑态，先复位 DOM
      updateShareChrome();
      content.innerHTML = '<p>' + I18N.t('share.invalid') + '</p>';
      clearToc($('share-toc'));
      return;
    }
    var draft = shareCtx ? shareCtx.draft : null;
    shareCtx = { user: parts[1], id: parts[2], sig: parts[3], title: '', mode: 'off', content: '', editing: false, draft: draft };
    // mode 此刻是 'off'：先按只读刷一次顶栏，加载失败时文案/按钮也是对的。
    updateShareChrome();
    fetch(shareApi(), { headers: { 'Accept-Language': I18N.lang() } })
      .then(function (r) { return r.json().then(function (d) { return { ok: r.ok, data: d }; }); })
      .then(function (res) {
        if (!res.ok || res.data.error) {
          content.innerHTML = '<p>' + I18N.t('share.gone') + '</p>';
          clearToc($('share-toc'));
          if (shareCtx.draft != null) { keepShareDraft(); return; }
          exitShareEdit(); // 链接失效/关闭编辑：别把残留编辑器留在页面上
          return;
        }
        var doc = res.data.doc || {};
        shareCtx.title = doc.title || '';
        shareCtx.mode = doc.share_mode || 'off';
        shareCtx.content = doc.content || '';
        content.innerHTML = res.data.html || '';
        buildToc(content, $('share-toc'), document.querySelector('.share-body'));
        updateShareChrome();
        refreshTitle();
        // 401 回来后草稿还在：链接仍可编辑就回填，已被降级成只读就复位 DOM（否则编辑器留在屏幕上）。
        if (shareCtx.draft != null) {
          if (shareCtx.mode === 'edit') { enterShareEdit(); } else { exitShareEdit(); }
        }
      })
      .catch(function () {
        content.innerHTML = '<p>' + I18N.t('share.load_failed') + '</p>';
        clearToc($('share-toc'));
        if (shareCtx.draft != null) { keepShareDraft(); return; }
        exitShareEdit();
      });
  }

  // 登录往返后重取分享失败（网络抖动 / 作者刚好关了分享）：草稿是用户唯一的一份内容，
  // 不能走 exitShareEdit 静默清掉，改为留在编辑态让用户自己复制。
  function keepShareDraft() {
    shareCtx.mode = 'edit';
    enterShareEdit();
    msg('share-msg', I18N.t('share.draft_kept'));
  }

  // 按分享模式刷新顶栏：可编辑时显示「编辑」按钮并改文案。
  function updateShareChrome() {
    // 不用 shareCtx 判空：链接还没解析出来（parts 不足）时也要把顶栏文案刷成当前语言。
    var editable = !!shareCtx && shareCtx.mode === 'edit';
    $('share-edit').hidden = !editable;
    $('share-brand-tip').textContent = I18N.t(editable ? 'share.brand_tip_edit' : 'share.brand_tip');
  }

  function enterShareEdit() {
    if (!shareCtx || shareCtx.mode !== 'edit') { return; }
    shareCtx.editing = true;
    $('share-source').value = shareCtx.draft != null ? shareCtx.draft : shareCtx.content;
    $('share-preview').innerHTML = $('share-content').innerHTML;
    $('share-content').hidden = true;
    $('share-toc').hidden = true;
    $('share-editor').hidden = false;
    document.querySelector('.share-body').classList.add('editing');
    $('share-source').focus();
  }

  function exitShareEdit() {
    if (!shareCtx) { return; }
    shareCtx.editing = false;
    shareCtx.draft = null;
    $('share-editor').hidden = true;
    $('share-content').hidden = false;
    document.querySelector('.share-body').classList.remove('editing');
    buildToc($('share-content'), $('share-toc'), document.querySelector('.share-body'));
  }

  function cancelShareEdit() {
    if (!shareCtx) { return; }
    if ($('share-source').value === shareCtx.content) { exitShareEdit(); return; }
    uiConfirm(I18N.t('doc.discard_confirm'), {
      title: I18N.t('doc.discard_title'), confirmText: I18N.t('doc.discard_ok'), danger: true
    }).then(function (ok) { if (ok) { exitShareEdit(); } });
  }

  function saveShareEdit() {
    if (!shareCtx) { return; }
    if (imgUploads > 0) { msg('share-msg', I18N.t('img.uploading_wait'), true); return; }
    var content = $('share-source').value;
    fetch(shareApi(), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'Accept-Language': I18N.lang() },
      body: JSON.stringify({ content: content })
    }).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (d) { return { status: r.status, data: d }; });
    }).then(function (res) {
      if (res.status === 401) {
        shareCtx.draft = content; // 放在 shareCtx 上，showLogin 里的 resetSession 不会清掉
        showLogin();
        return;
      }
      if (res.status === 404) {
        shareCtx.mode = 'off'; // 分享已关闭/失效：顶栏收起「编辑」，避免再点再失败
        exitShareEdit();
        updateShareChrome();
        uiAlert(I18N.t('share.gone_on_save'), { title: I18N.t('action.dialog_title') });
        return;
      }
      if (res.status !== 200) { throw new Error(res.data.error || I18N.t('share.save_failed')); }
      shareCtx.content = content;
      $('share-content').innerHTML = res.data.html || '';
      exitShareEdit();
      msg('share-msg', I18N.t('share.save_ok'));
    }).catch(function (e) {
      uiAlert(e.message || I18N.t('share.save_failed'), { title: I18N.t('action.dialog_title') });
    });
  }

  function shareApi() {
    return '/api/share/' + encodeURIComponent(shareCtx.user) + '/' + encodeURIComponent(shareCtx.id) + '/' + encodeURIComponent(shareCtx.sig);
  }

  function bindShareEvents() {
    bindDialog();
    $('share-theme-btn').addEventListener('click', toggleTheme);
    $('share-brand').addEventListener('click', function () { location.href = '/'; });
    $('share-clone').addEventListener('click', cloneShared);
    $('share-edit').addEventListener('click', enterShareEdit);
    $('share-edit-save').addEventListener('click', saveShareEdit);
    $('share-edit-cancel').addEventListener('click', cancelShareEdit);
    // 上传若因 401 触发 showLogin，resetSession 会清掉编辑器；先把正文存进 shareCtx.draft，
    // 回来时 setupShare 靠它重新进入编辑态（与 saveShareEdit 的处理一致）。
    var stashDraft = function () {
      if (shareCtx) { shareCtx.draft = $('share-source').value; }
    };
    bindImageInput($('share-source'), 'share-msg', stashDraft);
    $('share-img').addEventListener('click', function () { $('share-img-file').click(); });
    $('share-img-file').addEventListener('change', function () {
      insertImages($('share-source'), this.files, 'share-msg', stashDraft);
      this.value = '';
    });
    $('share-source').addEventListener('input', function () {
      var v = this.value;
      clearTimeout(sharePreviewTimer);
      sharePreviewTimer = setTimeout(function () {
        api('/api/preview', {
          method: 'POST',
          body: JSON.stringify({ content: v }),
          headers: { 'Content-Type': 'application/json' },
          noAuthRedirect: true
        }).then(function (r) { $('share-preview').innerHTML = r.html || ''; })
          .catch(function () {
            $('share-preview').innerHTML = '<p class="msg">' + I18N.t('share.preview_need_login') + '</p>';
          });
      }, 300);
    });
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
    // 与文档编辑器一致：编辑中刷新/关标签页会悄无声息地丢掉正文，先问一句。
    window.addEventListener('beforeunload', function (e) {
      if (shareCtx && shareCtx.editing && $('share-source').value !== shareCtx.content) {
        e.preventDefault();
        e.returnValue = '';
      }
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
    // 编辑态 #share-content 是 hidden 的，html2canvas 抓不可见元素只会得到空白，改导预览。
    var src = shareCtx && shareCtx.editing ? $('share-preview') : $('share-content');
    exportPdf(src, shareCtx ? shareCtx.title : 'doc').catch(function (e) {
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
    if (location.pathname.indexOf('/s/') === 0) {
      bindShareEvents(); // 只绑一次：setupShare 会被 resumeShare 二次调用
      bindLogin();
      setupShare();
      return;
    }
    bindEvents();
    bindLogin();
    refreshTitle();
    window.addEventListener('popstate', onPopState);
    api('/api/me').then(function (d) { enterApp(d.user); }).catch(showLogin);
  }

  boot();
})();
