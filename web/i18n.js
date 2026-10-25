/* MDBox 前端国际化：零依赖，中 / 英两套文案。
   语言来源：localStorage['mdbox:lang'] → navigator.language（zh* 记中文，其余英文）→ 英文。 */
(function (global) {
  'use strict';

  var DICT = {
    zh: {
      'app.title': 'MDBox · Markdown 知识库',

      'brand.slogan': '· 人写，Agent 也写',

      'login.promo.lead': '你的 Markdown 知识库，人写、Agent 也写',
      'login.promo.f1': '纯 .md 文件存储，随时导出带走',
      'login.promo.f2': '一键只读分享',
      'login.promo.mcp': '让 Agent 通过 MCP 直接读写你的库',
      'login.tip': '请登录后使用',
      'login.tip_register': '注册新账号',
      'login.username': '用户名',
      'login.password': '密码',
      'login.password2': '再次输入密码',
      'login.submit': '登录',
      'login.register': '注册',
      'login.to_register': '没有账号？注册',
      'login.to_login': '已有账号？登录',

      'share.brand_tip': '只读分享',
      'share.title': '分享',
      'share.invalid': '分享链接无效。',
      'share.gone': '分享不存在或已失效。',
      'share.load_failed': '加载失败。',
      'share.download_failed': '下载失败',
      'share.clone': '复制到我的库',
      'share.clone_failed': '复制失败',
      'share.brand_tip_edit': '可编辑分享',
      'share.edit': '编辑',
      'share.save': '保存',
      'share.cancel': '取消',
      'share.save_ok': '已保存',
      'share.save_failed': '保存失败',
      'share.gone_on_save': '分享已失效或已关闭编辑，内容未保存。',
      'share.draft_kept': '分享已失效或无法加载，你的内容仍保留在编辑器里，请复制保存',
      'share.edit_hint': '保存后对方立即看到，无版本历史',
      'share.preview_need_login': '登录后才有实时预览',

      'action.theme': '切换主题',
      'action.mcp': 'MCP 接入',
      'action.account': '账号',
      'action.change_password': '修改密码',
      'action.logout': '退出',
      'action.cancel': '取消',
      'action.save': '保存',
      'action.delete': '删除',
      'action.archive': '归档',
      'action.copy': '复制',
      'action.back': '返回',
      'action.edit': '编辑',
      'action.share': '分享',
      'action.new': '新建',
      'action.upload': '上传 .md',
      'action.download': '下载 ▾',
      'action.dl_md': '下载 Markdown (.md)',
      'action.dl_pdf': '导出 PDF (.pdf)',
      'action.reset_token': '重置 token',
      'action.ok': '确定',
      'action.got_it': '知道了',
      'action.dialog_title': '提示',

      'nav.title': '目录',
      'toc.title': '本页目录',
      'nav.open': '打开目录',
      'nav.categories': '分类',
      'nav.tags': '标签',
      'nav.archive': '归档',
      'nav.archived_docs': '已归档文档',
      'nav.all_docs': '全部文档',
      'nav.no_tags': '暂无标签',
      'nav.logout': '退出登录',

      'list.search_placeholder': '搜索标题、标签、正文…',
      'list.empty_lead': '还没有文档。',
      'list.empty_wide': '点「新建」写一篇，或把 .md 文件拖进这里。',
      'list.empty_narrow': '可在电脑上新建，或让 agent 通过 MCP 写入。',
      'list.status_archived': '已归档',
      'list.filter_category': '分类：{v}',
      'list.filter_tag': '标签：{v}',
      'list.filter_search': '搜索：{v}',
      'list.count': '{n} 篇',
      'overview.range': '近 {days} 天',
      'overview.label_new': '新增',
      'overview.label_updated': '更新',
      'overview.label_archived': '归档',
      'overview.label_agent': 'Agent 写入',
      'list.shared': '已分享',
      'list.delete_title': '删除',
      'list.delete_aria': '删除文档',

      'edit.title_placeholder': '标题',
      'edit.category_placeholder': '分类',
      'edit.tags_placeholder': '标签，逗号分隔',
      'edit.source_placeholder': '在这里写 Markdown…',

      'doc.untitled': '未命名文档',
      'doc.source': '来源',
      'doc.updated': '更新',
      'doc.open_failed': '打开文档失败',
      'doc.saving': '保存中…',
      'doc.delete_confirm': '确定要永久删除「{title}」吗？\n\n此操作不可恢复，文件会被真正删除（如果只是想暂时收起来，请改用「归档」）。',
      'doc.delete_title': '删除文档',
      'doc.delete_failed': '删除失败：{msg}',
      'doc.delete_failed_title': '删除失败',
      'doc.archive_confirm': '归档后可在左侧「归档」中找回，确定归档？',
      'doc.archive_title': '归档文档',
      'doc.discard_confirm': '有未保存的修改，确定放弃？',
      'doc.discard_title': '放弃修改',
      'doc.discard_ok': '放弃',

      'pdf.generating': '正在生成 PDF…',
      'pdf.load_failed': '加载 PDF 组件失败，请检查网络',
      'pdf.export_failed': '导出失败，请重试',
      'pdf.export_failed_title': '导出失败',

      'mcp.title': 'MCP 接入',
      'mcp.tip': 'mdbox 内置 MCP 服务器，把下面配置加入任意支持 MCP 的 agent 即可读写知识库。',
      'mcp.http_label': 'HTTP（远程 agent；token 属于你的账号，只能访问你自己的文档）',
      'mcp.stdio_label': 'stdio（本机 agent 直连）',
      'mcp.token_failed': '获取 token 失败',
      'mcp.reset_confirm': '重置后旧 token 立即失效，已配置的 agent 需要换成新 token。继续？',
      'mcp.reset_failed': '重置失败',

      'pwd.title': '修改密码',
      'pwd.old': '旧密码',
      'pwd.new': '新密码（8-72 位）',
      'pwd.new2': '再次输入新密码',
      'pwd.mismatch': '两次输入的新密码不一致',
      'pwd.changed': '密码已修改，其他设备上的登录已失效。',
      'pwd.failed': '修改失败',

      'sharemodal.title': '分享文档',
      'sharemodal.label': '分享方式',
      'sharemodal.mode_off': '关闭分享',
      'sharemodal.mode_view': '只读链接（无需登录）',
      'sharemodal.mode_edit': '可编辑链接（需登录）',
      'sharemodal.hint_off': '关闭后链接立即失效。',
      'sharemodal.hint_view': '任何人凭链接可只读查看，无需登录，无法编辑。',
      'sharemodal.hint_edit': '链接不变；任何登录用户凭链接可在分享页直接编辑，保存即改写本文档。无版本历史。',
      'sharemodal.link_label': '分享链接（固定地址，可随时关闭）',

      'error.need_login': '需要登录',
      'error.op_failed': '操作失败：{msg}',
      'error.op_failed_title': '操作失败',
      'auth.login_failed': '登录失败',
      'auth.register_failed': '注册失败',
      'auth.register_mismatch': '两次输入的密码不一致',
      'upload.failed': '上传失败：{msg}',
      'upload.failed_title': '上传失败',
      'copied': '已复制'
    },

    en: {
      'app.title': 'MDBox · Markdown Knowledge Base',

      'brand.slogan': '· Written by you and your agent',

      'login.promo.lead': 'Your Markdown knowledge base — written by you and your agent',
      'login.promo.f1': 'Plain .md files — export and take them anywhere',
      'login.promo.f2': 'One-click read-only sharing',
      'login.promo.mcp': 'Let agents read and write your library via MCP',
      'login.tip': 'Sign in to continue',
      'login.tip_register': 'Create an account',
      'login.username': 'Username',
      'login.password': 'Password',
      'login.password2': 'Confirm password',
      'login.submit': 'Sign in',
      'login.register': 'Sign up',
      'login.to_register': "Don't have an account? Sign up",
      'login.to_login': 'Already have an account? Sign in',

      'share.brand_tip': 'Read-only',
      'share.title': 'Shared',
      'share.invalid': 'Invalid share link.',
      'share.gone': 'This share link is invalid or has expired.',
      'share.load_failed': 'Failed to load.',
      'share.download_failed': 'Download failed',
      'share.clone': 'Copy to my library',
      'share.clone_failed': 'Copy failed',
      'share.brand_tip_edit': 'Editable',
      'share.edit': 'Edit',
      'share.save': 'Save',
      'share.cancel': 'Cancel',
      'share.save_ok': 'Saved',
      'share.save_failed': 'Save failed',
      'share.gone_on_save': 'This share link is invalid or editing was turned off. Nothing was saved.',
      'share.draft_kept': 'This share link no longer works, but your draft is kept in the editor — copy it before leaving',
      'share.edit_hint': 'Changes are visible immediately. No version history.',
      'share.preview_need_login': 'Log in to see the live preview',

      'action.theme': 'Toggle theme',
      'action.mcp': 'MCP access',
      'action.account': 'Account',
      'action.change_password': 'Change password',
      'action.logout': 'Sign out',
      'action.cancel': 'Cancel',
      'action.save': 'Save',
      'action.delete': 'Delete',
      'action.archive': 'Archive',
      'action.copy': 'Copy',
      'action.back': 'Back',
      'action.edit': 'Edit',
      'action.share': 'Share',
      'action.new': 'New',
      'action.upload': 'Upload .md',
      'action.download': 'Download ▾',
      'action.dl_md': 'Download Markdown (.md)',
      'action.dl_pdf': 'Export PDF (.pdf)',
      'action.reset_token': 'Reset token',
      'action.ok': 'OK',
      'action.got_it': 'Got it',
      'action.dialog_title': 'Notice',

      'nav.title': 'Contents',
      'toc.title': 'On this page',
      'nav.open': 'Open contents',
      'nav.categories': 'Categories',
      'nav.tags': 'Tags',
      'nav.archive': 'Archive',
      'nav.archived_docs': 'Archived documents',
      'nav.all_docs': 'All documents',
      'nav.no_tags': 'No tags yet',
      'nav.logout': 'Sign out',

      'list.search_placeholder': 'Search titles, tags, and content…',
      'list.empty_lead': 'No documents yet.',
      'list.empty_wide': 'Click New to write one, or drag .md files here.',
      'list.empty_narrow': 'Create one on your computer, or let an agent write via MCP.',
      'list.status_archived': 'Archived',
      'list.filter_category': 'Category: {v}',
      'list.filter_tag': 'Tag: {v}',
      'list.filter_search': 'Search: {v}',
      'list.count': '{n} documents',
      'overview.range': 'Last {days} days',
      'overview.label_new': 'New',
      'overview.label_updated': 'Updated',
      'overview.label_archived': 'Archived',
      'overview.label_agent': 'By agent',
      'list.shared': 'Shared',
      'list.delete_title': 'Delete',
      'list.delete_aria': 'Delete document',

      'edit.title_placeholder': 'Title',
      'edit.category_placeholder': 'Category',
      'edit.tags_placeholder': 'Tags, comma-separated',
      'edit.source_placeholder': 'Write Markdown here…',

      'doc.untitled': 'Untitled',
      'doc.source': 'Source',
      'doc.updated': 'Updated',
      'doc.open_failed': 'Failed to open document',
      'doc.saving': 'Saving…',
      'doc.delete_confirm': 'Permanently delete "{title}"?\n\nThis cannot be undone — the file will be truly deleted. To set it aside temporarily, use Archive instead.',
      'doc.delete_title': 'Delete document',
      'doc.delete_failed': 'Delete failed: {msg}',
      'doc.delete_failed_title': 'Delete failed',
      'doc.archive_confirm': 'Archived documents can be found later under Archive. Archive this one?',
      'doc.archive_title': 'Archive document',
      'doc.discard_confirm': 'Discard unsaved changes?',
      'doc.discard_title': 'Discard changes',
      'doc.discard_ok': 'Discard',

      'pdf.generating': 'Generating PDF…',
      'pdf.load_failed': 'Failed to load the PDF component, please check your network',
      'pdf.export_failed': 'Export failed, please try again',
      'pdf.export_failed_title': 'Export failed',

      'mcp.title': 'MCP access',
      'mcp.tip': 'mdbox has a built-in MCP server. Add the config below to any MCP-capable agent to read and write your knowledge base.',
      'mcp.http_label': 'HTTP (remote agents; the token belongs to your account and only reaches your own documents)',
      'mcp.stdio_label': 'stdio (local agents connect directly)',
      'mcp.token_failed': 'Failed to get token',
      'mcp.reset_confirm': 'Resetting immediately invalidates the old token; configured agents must switch to the new one. Continue?',
      'mcp.reset_failed': 'Reset failed',

      'pwd.title': 'Change password',
      'pwd.old': 'Current password',
      'pwd.new': 'New password (8-72 characters)',
      'pwd.new2': 'Confirm new password',
      'pwd.mismatch': 'The new passwords do not match',
      'pwd.changed': 'Password changed. Other devices have been signed out.',
      'pwd.failed': 'Change failed',

      'sharemodal.title': 'Share document',
      'sharemodal.label': 'Sharing',
      'sharemodal.mode_off': 'Off',
      'sharemodal.mode_view': 'Read-only link (no login)',
      'sharemodal.mode_edit': 'Editable link (login required)',
      'sharemodal.hint_off': 'The link stops working immediately.',
      'sharemodal.hint_view': 'Anyone with the link can read, without logging in. No editing.',
      'sharemodal.hint_edit': 'Same link; any logged-in user can edit in place and save over this document. No version history.',
      'sharemodal.link_label': 'Share link (stable URL, can be turned off anytime)',

      'error.need_login': 'Sign-in required',
      'error.op_failed': 'Operation failed: {msg}',
      'error.op_failed_title': 'Operation failed',
      'auth.login_failed': 'Sign-in failed',
      'auth.register_failed': 'Sign-up failed',
      'auth.register_mismatch': 'Passwords do not match',
      'upload.failed': 'Upload failed: {msg}',
      'upload.failed_title': 'Upload failed',
      'copied': 'Copied'
    }
  };

  var DEFAULT = 'en';
  var current = DEFAULT;

  function detect() {
    var saved = null;
    try { saved = localStorage.getItem('mdbox:lang'); } catch (e) { /* ignore */ }
    if (saved === 'zh' || saved === 'en') { return saved; }
    var nav = (navigator.language || navigator.userLanguage || '').toLowerCase();
    return nav.indexOf('zh') === 0 ? 'zh' : 'en';
  }

  function t(key, vars) {
    var s = (DICT[current] || DICT[DEFAULT])[key];
    if (s == null) { s = DICT[DEFAULT][key]; }
    if (s == null) { return key; }
    if (vars) {
      s = s.replace(/\{(\w+)\}/g, function (m, name) {
        return vars[name] != null ? vars[name] : m;
      });
    }
    return s;
  }

  function lang() { return current; }

  function setLang(l) {
    if (l !== 'zh' && l !== 'en') { return; }
    current = l;
    try { localStorage.setItem('mdbox:lang', l); } catch (e) { /* ignore */ }
  }

  // applyStatic 把 [data-i18n*] 属性按当前语言写回 DOM，并同步 <html lang>。
  // 只处理叶子元素（写 textContent 会覆盖子节点）；混合文本需在 HTML 里单独包一层。
  function applyStatic(root) {
    root = root || document;
    var map = {
      'data-i18n': 'textContent',
      'data-i18n-title': 'title',
      'data-i18n-placeholder': 'placeholder',
      'data-i18n-aria': 'aria-label'
    };
    Object.keys(map).forEach(function (attr) {
      var prop = map[attr];
      var nodes = root.querySelectorAll('[' + attr + ']');
      for (var i = 0; i < nodes.length; i++) {
        var v = t(nodes[i].getAttribute(attr));
        if (prop === 'textContent') { nodes[i].textContent = v; }
        else { nodes[i].setAttribute(prop, v); }
      }
    });
    document.documentElement.setAttribute('lang', current === 'zh' ? 'zh-CN' : 'en');
  }

  current = detect();

  global.I18N = {
    t: t,
    lang: lang,
    setLang: setLang,
    applyStatic: applyStatic,
    detect: detect
  };
})(window);
